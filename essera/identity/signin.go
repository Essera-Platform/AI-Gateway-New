package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// FlowPurpose is the auth flow that holds a sign-in between two steps.
	FlowPurpose = "essera_sign_in"
	// FlowTTL matches the life of a Cognito challenge session.
	FlowTTL = 3 * time.Minute

	// At most this many sign-in steps per email and per IP address in a ThrottleWindow, across
	// every node; the IP allows for an office behind one address.
	MaxAttemptsPerEmail = 10
	MaxAttemptsPerIP    = 50
	ThrottleWindow      = 15 * time.Minute

	NextCode        = "code"
	NextNewPassword = "new_password"
)

var (
	// ErrSignInFailed is the one answer to a wrong email, password or code, a spent or replayed
	// step, an unknown or disabled user and a person who is not active, so the answer never
	// tells whether an account exists.
	ErrSignInFailed = errors.New("The email, password or code is not correct.")
	// ErrStepExpired is told for every pending sign-in that has run out, an unknown email's
	// among them, so it tells nothing about the account either.
	ErrStepExpired = errors.New("The code has expired. Send a new code.")
	ErrThrottled   = errors.New("Too many sign-in attempts. Try again later.")
	ErrUnavailable = errors.New("Signing in is not available right now. Try again later.")
)

// Outcome is where a sign-in step led: the next step to ask for, with the opaque id of the
// pending sign-in (kept in an HttpOnly cookie, never in the page), or the signed-in user with
// what binds their session to Cognito.
type Outcome struct {
	Next    string
	Flow    string
	Problem string // why a new password was not accepted; the step can be retried

	User         *model.User
	Sub          string
	RefreshToken string
}

// pending is what the server keeps between two steps; the Cognito session is sealed.
type pending struct {
	Email     string `json:"email"`
	Username  string `json:"username"`
	Challenge string `json:"challenge"`
	Session   string `json:"session"`
	Decoy     bool   `json:"decoy,omitempty"` // no such sign-in exists; the next step fails
}

// Start begins a sign-in with an email, and the password in password mode. In code mode an
// email Cognito refuses still asks for a code, so the answer is the same for every address.
func Start(ctx context.Context, email, password, ip string, now time.Time) (*Outcome, error) {
	client, ok := signInClient()
	if !ok {
		return nil, ErrUnavailable
	}
	email = model.NormalizeEmail(email)
	if err := throttle(ip, email, now); err != nil {
		return nil, err
	}
	mode := settings.SignInMode()
	if email == "" || len(email) > 254 || (mode == settings.SignInPassword && password == "") {
		return nil, ErrSignInFailed
	}
	preferred := "PASSWORD"
	if mode == settings.SignInEmailOTP {
		preferred = "EMAIL_OTP"
	}
	parameters := map[string]string{"USERNAME": email, "SECRET_HASH": client.secretHash(email), "PREFERRED_CHALLENGE": preferred}
	if mode == settings.SignInPassword {
		parameters["PASSWORD"] = password
	}
	var answer challengeAnswer
	err := client.call(ctx, "InitiateAuth", map[string]any{"AuthFlow": "USER_AUTH", "ClientId": client.ClientID, "AuthParameters": parameters}, &answer)
	state := pending{Email: email, Username: email}
	var failure *cognitoError
	if mode == settings.SignInEmailOTP && errors.As(err, &failure) && failure.refused() {
		logRefusal(email, failure.Type)
		state.Decoy = true
		return keep(state, now)
	}
	if err != nil {
		return nil, failed(email, err)
	}
	return advance(ctx, client, state, answer, password, now)
}

// Continue answers the pending step with the code or the new password. The pending sign-in is
// spent whatever happens, so a step cannot be replayed; a new password the pool's policy
// refuses can be chosen again.
func Continue(ctx context.Context, flow, response, ip string, now time.Time) (*Outcome, error) {
	client, ok := signInClient()
	if !ok {
		return nil, ErrUnavailable
	}
	spent, err := model.ConsumeAuthFlow(flow, model.AuthFlowMatch{Purpose: FlowPurpose})
	if err != nil {
		if throttled := throttle(ip, "", now); throttled != nil {
			return nil, throttled
		}
		if errors.Is(err, model.ErrAuthFlowExpired) {
			return nil, ErrStepExpired
		}
		return nil, ErrSignInFailed
	}
	var state pending
	if err := common.UnmarshalJsonStr(spent.Payload, &state); err != nil {
		return nil, ErrSignInFailed
	}
	if err := throttle(ip, state.Email, now); err != nil {
		return nil, err
	}
	if state.Decoy || response == "" {
		return nil, ErrSignInFailed
	}
	session, err := open(state.Session)
	if err != nil {
		return nil, ErrSignInFailed
	}
	state.Session = session
	responses := map[string]string{}
	switch state.Challenge {
	case "EMAIL_OTP":
		responses["EMAIL_OTP_CODE"] = response
	case "NEW_PASSWORD_REQUIRED":
		responses["NEW_PASSWORD"] = response
	default:
		return nil, ErrSignInFailed
	}
	answer, err := client.respond(ctx, state, state.Challenge, session, responses)
	var failure *cognitoError
	if state.Challenge == "NEW_PASSWORD_REQUIRED" && errors.As(err, &failure) && failure.Type == "InvalidPasswordException" {
		outcome, err := keep(state, now)
		if err == nil {
			outcome.Problem = "The new password does not meet the password policy."
		}
		return outcome, err
	}
	if err != nil {
		return nil, failed(state.Email, err)
	}
	return advance(ctx, client, state, answer, "", now)
}

func (p poolClient) respond(ctx context.Context, state pending, challenge, session string, responses map[string]string) (challengeAnswer, error) {
	responses["USERNAME"] = state.Username
	responses["SECRET_HASH"] = p.secretHash(state.Username)
	var answer challengeAnswer
	err := p.call(ctx, "RespondToAuthChallenge", map[string]any{
		"ChallengeName": challenge, "ClientId": p.ClientID, "Session": session, "ChallengeResponses": responses,
	}, &answer)
	return answer, err
}

// advance follows Cognito's answers until it signs the person in or needs them: the password
// is answered here, within the request that carried it, and never kept.
func advance(ctx context.Context, client poolClient, state pending, answer challengeAnswer, password string, now time.Time) (*Outcome, error) {
	for range 4 {
		if answer.AuthenticationResult != nil {
			return finish(ctx, client, state.Email, answer.AuthenticationResult, now)
		}
		if username := answer.ChallengeParameters["USERNAME"]; username != "" {
			state.Username = username
		} else if username := answer.ChallengeParameters["USER_ID_FOR_SRP"]; username != "" {
			state.Username = username
		}
		state.Challenge, state.Session = answer.ChallengeName, answer.Session
		var err error
		switch answer.ChallengeName {
		case "EMAIL_OTP", "NEW_PASSWORD_REQUIRED":
			return keep(state, now)
		case "SELECT_CHALLENGE":
			responses := map[string]string{"ANSWER": "EMAIL_OTP"}
			if password != "" {
				responses = map[string]string{"ANSWER": "PASSWORD", "PASSWORD": password}
			}
			answer, err = client.respond(ctx, state, "SELECT_CHALLENGE", answer.Session, responses)
		case "PASSWORD", "PASSWORD_SRP":
			if password == "" {
				return nil, ErrSignInFailed
			}
			answer, err = client.respond(ctx, state, "PASSWORD", answer.Session, map[string]string{"PASSWORD": password})
		default:
			common.SysLog(common.LogText("essera: sign-in of %s stopped at Cognito challenge %s", emailDigest(state.Email), answer.ChallengeName))
			return nil, ErrSignInFailed
		}
		if err != nil {
			return nil, failed(state.Email, err)
		}
	}
	return nil, ErrSignInFailed
}

// keep saves a pending step for the next request and returns its opaque id.
func keep(state pending, now time.Time) (*Outcome, error) {
	if state.Session != "" {
		sealed, err := seal(state.Session)
		if err != nil {
			return nil, err
		}
		state.Session = sealed
	}
	payload, err := common.Marshal(state)
	if err != nil {
		return nil, err
	}
	token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: FlowPurpose, Payload: string(payload), ExpiresAt: now.Add(FlowTTL)})
	if err != nil {
		return nil, err
	}
	next := NextCode
	if state.Challenge == "NEW_PASSWORD_REQUIRED" {
		next = NextNewPassword
	}
	return &Outcome{Next: next, Flow: token}, nil
}

// finish checks the tokens of a completed sign-in and finds the person's user.
func finish(ctx context.Context, client poolClient, email string, result *authenticationResult, now time.Time) (*Outcome, error) {
	sub, err := verifyIDToken(ctx, client, result.IdToken, now)
	if err != nil {
		return nil, failed(email, err)
	}
	if result.RefreshToken == "" {
		return nil, failed(email, refuse("Cognito returned no refresh token"))
	}
	user, err := personUser(sub)
	if err != nil {
		return nil, failed(email, err)
	}
	return &Outcome{User: user, Sub: sub, RefreshToken: result.RefreshToken}, nil
}

// failed turns why a sign-in failed into the answer the browser gets, and logs the reason
// without the password, the code or any token.
func failed(email string, err error) error {
	var failure *cognitoError
	var refusal *Refusal
	switch {
	case errors.As(err, &failure) && failure.refused():
		logRefusal(email, failure.Type)
		return ErrSignInFailed
	case errors.As(err, &refusal):
		logRefusal(email, refusal.Reason)
		return ErrSignInFailed
	case errors.As(err, &failure) && failure.Type == "TooManyRequestsException":
		return ErrThrottled
	}
	common.SysError(common.LogText("essera: sign-in of %s: %s", emailDigest(email), err.Error()))
	return ErrUnavailable
}

func logRefusal(email, reason string) {
	common.SysLog(common.LogText("essera: sign-in refused for %s: %s", emailDigest(email), reason))
}

// emailDigest names an email in logs and counters without writing it.
func emailDigest(email string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(email)))
	return hex.EncodeToString(digest[:8])
}

var lastSwept atomic.Int64

// throttle counts a sign-in step against the IP and the email; past either limit it is refused,
// whether or not the account exists.
func throttle(ip, email string, now time.Time) error {
	window := now.Truncate(ThrottleWindow)
	subjects := map[string]int64{"ip:" + ip: MaxAttemptsPerIP}
	if email != "" {
		subjects["email:"+emailDigest(email)] = MaxAttemptsPerEmail
	}
	refused := false
	for subject, limit := range subjects {
		row := records.SignInAttempt{Subject: subject, Started: window.Unix(), Attempts: 1}
		err := model.DB.Clauses(
			clause.OnConflict{
				Columns:   []clause.Column{{Name: "subject"}, {Name: "started"}},
				DoUpdates: clause.Assignments(map[string]any{"attempts": gorm.Expr("essera_sign_in_attempts.attempts + 1")}),
			},
			clause.Returning{Columns: []clause.Column{{Name: "attempts"}}},
		).Create(&row).Error
		if err != nil {
			return ErrUnavailable
		}
		refused = refused || row.Attempts > limit
	}
	if previous := lastSwept.Swap(window.Unix()); previous != window.Unix() {
		model.DB.Where("started < ?", window.Add(-ThrottleWindow).Unix()).Delete(&records.SignInAttempt{})
	}
	if refused {
		return ErrThrottled
	}
	return nil
}
