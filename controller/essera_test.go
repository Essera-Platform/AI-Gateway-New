// Essera's tests of the sign-in of the Console's people (CHANGES-ESSERA.md): the Gateway's own
// sign-in against a stand-in of the Cognito user-pool API, the Gateway session bound to the
// Cognito sign-in, refresh and sign-out.
package controller

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/identity"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/probes"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	cognitoIssuer = "https://cognito-idp.us-east-1.amazonaws.com/" + snapshottest.UserPoolID
	cognitoClient = "app-web-dev"
	cognitoSecret = "client-secret"
	goodCode      = "123456"
)

type cognitoUser struct {
	sub, password string
	disabled      bool
	mustChange    bool
}

type cognitoSession struct {
	email, challenge string
	expired          bool
}

// fakeCognito answers InitiateAuth, RespondToAuthChallenge, GetTokensFromRefreshToken and
// RevokeToken as the user pool does for an app client with a secret, refresh-token rotation
// and prevented user-existence errors.
type fakeCognito struct {
	t        *testing.T
	key      *rsa.PrivateKey
	mu       sync.Mutex
	users    map[string]*cognitoUser
	sessions map[string]*cognitoSession
	refresh  map[string]string // live refresh token → sub
	revoked  []string
	// forge, when set, changes the ID tokens the pool returns, as a compromised or misdirected
	// endpoint could.
	forge func(claims jwt.MapClaims) *rsa.PrivateKey
}

func (f *fakeCognito) secretHash(username string) string {
	mac := hmac.New(sha256.New, []byte(cognitoSecret))
	mac.Write([]byte(username + cognitoClient))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (f *fakeCognito) tokens(sub string) map[string]any {
	claims := jwt.MapClaims{
		"iss": cognitoIssuer, "aud": cognitoClient, "sub": sub, "token_use": "id",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	}
	signer := f.key
	if f.forge != nil {
		if key := f.forge(claims); key != nil {
			signer = key
		}
	}
	idToken, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(signer)
	require.NoError(f.t, err)
	refreshToken := common.GetRandomString(40)
	f.refresh[refreshToken] = sub
	return map[string]any{"AuthenticationResult": map[string]any{"IdToken": idToken, "AccessToken": "access", "RefreshToken": refreshToken, "ExpiresIn": 3600}}
}

func (f *fakeCognito) challenge(email, name string) map[string]any {
	session := common.GetRandomString(32)
	f.sessions[session] = &cognitoSession{email: email, challenge: name}
	return map[string]any{"ChallengeName": name, "Session": session, "ChallengeParameters": map[string]string{"USERNAME": f.users[email].sub}}
}

func (f *fakeCognito) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var input map[string]any
	data, _ := io.ReadAll(r.Body)
	require.NoError(f.t, common.Unmarshal(data, &input))
	stringsOf := func(key string) map[string]string {
		values := map[string]string{}
		raw, _ := input[key].(map[string]any)
		for name, value := range raw {
			values[name], _ = value.(string)
		}
		return values
	}
	answer := func(status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(status)
		encoded, _ := common.Marshal(body)
		_, _ = w.Write(encoded)
	}
	refuse := func(kind string) {
		answer(http.StatusBadRequest, map[string]any{"__type": "com.amazonaws.cognito#" + kind, "message": kind})
	}
	if input["ClientId"] != cognitoClient {
		refuse("ResourceNotFoundException")
		return
	}
	switch strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AWSCognitoIdentityProviderService.") {
	case "InitiateAuth":
		parameters := stringsOf("AuthParameters")
		email := parameters["USERNAME"]
		require.Equal(f.t, f.secretHash(email), parameters["SECRET_HASH"], "SECRET_HASH")
		user := f.users[email]
		if user == nil || user.disabled {
			refuse("NotAuthorizedException")
			return
		}
		if parameters["PREFERRED_CHALLENGE"] == "EMAIL_OTP" {
			answer(http.StatusOK, f.challenge(email, "EMAIL_OTP"))
			return
		}
		if parameters["PASSWORD"] != user.password {
			refuse("NotAuthorizedException")
			return
		}
		if user.mustChange {
			answer(http.StatusOK, f.challenge(email, "NEW_PASSWORD_REQUIRED"))
			return
		}
		answer(http.StatusOK, f.tokens(user.sub))
	case "RespondToAuthChallenge":
		session := f.sessions[input["Session"].(string)]
		delete(f.sessions, input["Session"].(string))
		responses := stringsOf("ChallengeResponses")
		if session == nil || session.challenge != input["ChallengeName"] {
			refuse("NotAuthorizedException")
			return
		}
		user := f.users[session.email]
		require.Equal(f.t, user.sub, responses["USERNAME"], "the pool's username from the challenge")
		require.Equal(f.t, f.secretHash(user.sub), responses["SECRET_HASH"])
		switch {
		case session.expired:
			refuse("ExpiredCodeException")
		case session.challenge == "EMAIL_OTP" && responses["EMAIL_OTP_CODE"] != goodCode:
			refuse("CodeMismatchException")
		case session.challenge == "NEW_PASSWORD_REQUIRED" && len(responses["NEW_PASSWORD"]) < 12:
			f.sessions[input["Session"].(string)] = session // a weak password can be chosen again
			refuse("InvalidPasswordException")
		default:
			if session.challenge == "NEW_PASSWORD_REQUIRED" {
				user.password, user.mustChange = responses["NEW_PASSWORD"], false
			}
			answer(http.StatusOK, f.tokens(user.sub))
		}
	case "GetTokensFromRefreshToken":
		require.Equal(f.t, cognitoSecret, input["ClientSecret"])
		token, _ := input["RefreshToken"].(string)
		sub, live := f.refresh[token]
		if !live || f.disabledSub(sub) {
			refuse("NotAuthorizedException")
			return
		}
		delete(f.refresh, token) // rotation: the old refresh token is spent
		answer(http.StatusOK, f.tokens(sub))
	case "RevokeToken":
		require.Equal(f.t, cognitoSecret, input["ClientSecret"])
		token, _ := input["Token"].(string)
		delete(f.refresh, token)
		f.revoked = append(f.revoked, token)
		answer(http.StatusOK, map[string]any{})
	default:
		refuse("UnknownOperationException")
	}
}

func (f *fakeCognito) disabledSub(sub string) bool {
	for _, user := range f.users {
		if user.sub == sub {
			return user.disabled
		}
	}
	return true
}

func (f *fakeCognito) setSessionsExpired() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, session := range f.sessions {
		session.expired = true
	}
}

// esseraSignInSetup applies the sample configuration (whose people become users) and stands
// up the user pool: the end user and the owner of CustomerID, the suspended person, a pool user
// who is no person, and a disabled one.
func esseraSignInSetup(t *testing.T, mode string) *fakeCognito {
	t.Helper()
	require.NoError(t, i18n.Init())
	t.Setenv("ESSERA_MANAGED", "true")
	t.Setenv("ESSERA_SIGN_IN", mode)
	t.Setenv("ESSERA_OIDC_ISSUER", cognitoIssuer)
	t.Setenv("ESSERA_OIDC_CLIENT_ID", cognitoClient)
	t.Setenv("ESSERA_OIDC_CLIENT_SECRET", cognitoSecret)
	recordstest.UseDatabase(t, &model.User{}, &model.UserSession{}, &model.AuthFlow{}, &model.AuditLog{}, &model.TwoFA{}, &model.PasskeyCredential{},
		&model.Channel{}, &model.Ability{}, &model.SystemTask{}, &model.Token{})
	previousLogDB, previousMain, previousLog := model.LOG_DB, common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousSecret := common.RedisEnabled, common.SessionSecret
	model.LOG_DB = model.DB
	dbType := common.DatabaseTypeSQLite
	if os.Getenv("ESSERA_TEST_POSTGRES_DSN") != "" {
		dbType = common.DatabaseTypePostgreSQL
	}
	common.SetDatabaseTypes(dbType, dbType)
	common.RedisEnabled, common.SessionSecret = false, "essera-sign-in-test-secret"
	snapshot.Activate(nil)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	previousKeys, previousEndpoint := auth.KeySetFor, identity.CognitoEndpoint
	auth.KeySetFor = func(string) oidc.KeySet { return &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}} }
	pool := &fakeCognito{t: t, key: key, sessions: map[string]*cognitoSession{}, refresh: map[string]string{}, users: map[string]*cognitoUser{
		"user-1@acme.example":  {sub: snapshottest.UserSub, password: "Correct-Horse-1"},
		"owner@acme.example":   {sub: snapshottest.AdminSub, password: "Correct-Horse-2", mustChange: true},
		"gone@acme.example":    {sub: snapshottest.SuspendedSub, password: "Correct-Horse-3"},
		"stranger@example.com": {sub: "sub-stranger", password: "Correct-Horse-4"},
		"locked@acme.example":  {sub: "sub-locked", password: "Correct-Horse-5", disabled: true},
	}}
	server := httptest.NewServer(pool)
	identity.CognitoEndpoint = func(string) string { return server.URL }
	t.Cleanup(func() {
		server.Close()
		auth.KeySetFor, identity.CognitoEndpoint = previousKeys, previousEndpoint
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.SessionSecret = previousRedis, previousSecret
		snapshot.Activate(nil)
	})
	document, err := common.Marshal(snapshottest.Sample())
	require.NoError(t, err)
	require.NoError(t, configsync.Apply(3, "digest", document))
	return pool
}

type gatewayResponse struct {
	recorder *httptest.ResponseRecorder
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	Data     struct {
		Next        string `json:"next"`
		AccessToken string `json:"access_token"`
		Session     struct {
			SID string `json:"sid"`
		} `json:"session"`
	} `json:"data"`
}

func (r gatewayResponse) cookie(name string) *http.Cookie {
	for _, cookie := range r.recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func call(t *testing.T, path, body string, cookies ...*http.Cookie) gatewayResponse {
	t.Helper()
	engine := gin.New()
	engine.POST("/api/essera/sign-in", EsseraSignIn)
	engine.POST("/api/essera/sign-in/continue", EsseraSignInContinue)
	engine.POST("/api/user/auth/refresh", RefreshAuth)
	engine.POST("/api/user/auth/logout", AuthLogout)
	engine.POST("/api/user/login", Login)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "203.0.113.7:1234"
	for _, cookie := range cookies {
		if cookie != nil {
			request.AddCookie(cookie)
		}
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	response := gatewayResponse{recorder: recorder}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func signInWithPassword(t *testing.T, email, password string) gatewayResponse {
	t.Helper()
	return call(t, "/api/essera/sign-in", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}

// signInWithCode signs a person in as Dev does: their email, then the code Cognito emailed.
func signInWithCode(t *testing.T, email string) gatewayResponse {
	t.Helper()
	started := call(t, "/api/essera/sign-in", fmt.Sprintf(`{"email":%q}`, email))
	require.True(t, started.Success, started.recorder.Body.String())
	require.Equal(t, identity.NextCode, started.Data.Next)
	return call(t, "/api/essera/sign-in/continue", fmt.Sprintf(`{"code":%q}`, goodCode), started.cookie("essera_sign_in"))
}

func storedRefreshToken(t *testing.T, sid string) string {
	t.Helper()
	var row records.CognitoSession
	require.NoError(t, model.DB.Where("session_id = ?", sid).First(&row).Error)
	return row.RefreshToken
}

func TestAnActivePersonSignsInWithTheirPasswordAndTheSessionIsBoundToCognito(t *testing.T) {
	pool := esseraSignInSetup(t, "password")

	response := signInWithPassword(t, "User-1@Acme.example", "Correct-Horse-1")

	require.True(t, response.Success, response.recorder.Body.String())
	assert.NotEmpty(t, response.Data.AccessToken)
	refreshCookie := response.cookie(service.RefreshCookieName)
	require.NotNil(t, refreshCookie, "the Gateway's own session and refresh cookie")
	assert.True(t, refreshCookie.HttpOnly)
	sealed := storedRefreshToken(t, response.Data.Session.SID)
	for token := range pool.refresh {
		assert.NotContains(t, sealed, token, "the Cognito refresh token is kept sealed")
		assert.NotContains(t, response.recorder.Body.String(), token, "and never reaches the browser")
	}
	var user model.User
	require.NoError(t, model.DB.Where("oidc_id = ?", snapshottest.UserSub).First(&user).Error)
	sessions, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
	require.NoError(t, err)
	assert.EqualValues(t, 1, sessions)
}

func TestEveryFailedSignInLooksTheSame(t *testing.T) {
	esseraSignInSetup(t, "password")

	cases := map[string][2]string{
		"wrong password":          {"user-1@acme.example", "wrong"},
		"unknown email":           {"nobody@acme.example", "Correct-Horse-1"},
		"disabled in Cognito":     {"locked@acme.example", "Correct-Horse-5"},
		"suspended person":        {"gone@acme.example", "Correct-Horse-3"},
		"pool user but no person": {"stranger@example.com", "Correct-Horse-4"},
	}
	var bodies []string
	for name, attempt := range cases {
		response := signInWithPassword(t, attempt[0], attempt[1])
		assert.False(t, response.Success, name)
		assert.Equal(t, http.StatusUnauthorized, response.recorder.Code, name)
		assert.Nil(t, response.cookie(service.RefreshCookieName), name)
		bodies = append(bodies, response.recorder.Body.String())
	}
	for _, body := range bodies {
		assert.Equal(t, bodies[0], body, "nothing tells which accounts exist")
	}
	var sessions int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessions).Error)
	assert.Zero(t, sessions)
}

func TestTheFirstSignInSetsANewPasswordAndItsStepCannotBeReplayed(t *testing.T) {
	esseraSignInSetup(t, "password")

	started := signInWithPassword(t, "owner@acme.example", "Correct-Horse-2")
	require.True(t, started.Success, started.recorder.Body.String())
	assert.Equal(t, identity.NextNewPassword, started.Data.Next)
	flow := started.cookie("essera_sign_in")
	require.NotNil(t, flow)
	assert.True(t, flow.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, flow.SameSite)
	assert.NotContains(t, started.recorder.Body.String(), "Session", "the Cognito session stays on the server")

	weak := call(t, "/api/essera/sign-in/continue", `{"new_password":"short"}`, flow)
	assert.False(t, weak.Success)
	assert.Equal(t, identity.NextNewPassword, weak.Data.Next, "a password the policy refuses can be chosen again")
	retry := weak.cookie("essera_sign_in")
	require.NotNil(t, retry)

	replayed := call(t, "/api/essera/sign-in/continue", `{"new_password":"Another-Horse-22"}`, flow)
	assert.Equal(t, http.StatusUnauthorized, replayed.recorder.Code, "a spent step is refused")

	done := call(t, "/api/essera/sign-in/continue", `{"new_password":"Another-Horse-22"}`, retry)
	require.True(t, done.Success, done.recorder.Body.String())
	assert.NotEmpty(t, done.Data.AccessToken)

	again := call(t, "/api/essera/sign-in/continue", `{"new_password":"Another-Horse-33"}`, retry)
	assert.Equal(t, http.StatusUnauthorized, again.recorder.Code, "and so is a completed one")
}

func TestAnEmailedCodeSignsInOnceAndOnlyWhenRight(t *testing.T) {
	pool := esseraSignInSetup(t, "email_otp")
	start := func(email string) gatewayResponse {
		response := call(t, "/api/essera/sign-in", fmt.Sprintf(`{"email":%q}`, email))
		require.True(t, response.Success, response.recorder.Body.String())
		require.Equal(t, identity.NextCode, response.Data.Next)
		return response
	}

	unknown := start("nobody@acme.example")
	known := start("user-1@acme.example")
	assert.Equal(t, known.recorder.Body.String(), unknown.recorder.Body.String(), "an unknown email is asked for a code too")
	failed := call(t, "/api/essera/sign-in/continue", `{"code":"123456"}`, unknown.cookie("essera_sign_in"))
	assert.Equal(t, http.StatusUnauthorized, failed.recorder.Code)

	wrong := call(t, "/api/essera/sign-in/continue", `{"code":"000000"}`, known.cookie("essera_sign_in"))
	assert.Equal(t, http.StatusUnauthorized, wrong.recorder.Code)
	assert.Equal(t, failed.recorder.Body.String(), wrong.recorder.Body.String())

	expiring := start("user-1@acme.example")
	pool.setSessionsExpired()
	expired := call(t, "/api/essera/sign-in/continue", `{"code":"123456"}`, expiring.cookie("essera_sign_in"))
	assert.Equal(t, failed.recorder.Body.String(), expired.recorder.Body.String(), "an expired code")

	suspended := start("gone@acme.example")
	refused := call(t, "/api/essera/sign-in/continue", `{"code":"123456"}`, suspended.cookie("essera_sign_in"))
	assert.Equal(t, failed.recorder.Body.String(), refused.recorder.Body.String(), "a suspended person")

	right := start("user-1@acme.example")
	signedIn := call(t, "/api/essera/sign-in/continue", `{"code":"123456"}`, right.cookie("essera_sign_in"))
	require.True(t, signedIn.Success, signedIn.recorder.Body.String())
	assert.NotEmpty(t, signedIn.Data.AccessToken)
	replayed := call(t, "/api/essera/sign-in/continue", `{"code":"123456"}`, right.cookie("essera_sign_in"))
	assert.Equal(t, failed.recorder.Body.String(), replayed.recorder.Body.String(), "a replayed step")
}

func TestSignInIsThrottledPerEmailAndPerAddress(t *testing.T) {
	esseraSignInSetup(t, "password")

	for range identity.MaxAttemptsPerEmail {
		assert.Equal(t, http.StatusUnauthorized, signInWithPassword(t, "user-1@acme.example", "wrong").recorder.Code)
	}
	throttled := signInWithPassword(t, "user-1@acme.example", "Correct-Horse-1")
	assert.Equal(t, http.StatusTooManyRequests, throttled.recorder.Code, "even the right password, until the window ends")

	for i := identity.MaxAttemptsPerEmail + 1; i < identity.MaxAttemptsPerIP; i++ {
		signInWithPassword(t, fmt.Sprintf("someone-%d@acme.example", i), "wrong")
	}
	assert.Equal(t, http.StatusTooManyRequests, signInWithPassword(t, "owner@acme.example", "Correct-Horse-2").recorder.Code, "one address, many emails")
}

func TestARefreshRotatesTheCognitoTokenAndAReplayedOneEndsTheSession(t *testing.T) {
	pool := esseraSignInSetup(t, "email_otp")
	signedIn := signInWithCode(t, "user-1@acme.example")
	require.True(t, signedIn.Success)
	sid := signedIn.Data.Session.SID
	before := storedRefreshToken(t, sid)
	live := len(pool.refresh)

	refreshed := call(t, "/api/user/auth/refresh", `{}`, signedIn.cookie(service.RefreshCookieName))

	require.True(t, refreshed.Success, refreshed.recorder.Body.String())
	assert.NotEqual(t, before, storedRefreshToken(t, sid), "the rotated refresh token replaces the old one")
	assert.Len(t, pool.refresh, live, "and the old one is spent at Cognito")

	require.NoError(t, model.DB.Model(&records.CognitoSession{}).Where("session_id = ?", sid).Update("refresh_token", before).Error)
	replayed := call(t, "/api/user/auth/refresh", `{}`, refreshed.cookie(service.RefreshCookieName))
	assert.Equal(t, http.StatusUnauthorized, replayed.recorder.Code, "Cognito refuses the old refresh token")
	cleared := replayed.cookie(service.RefreshCookieName)
	require.NotNil(t, cleared)
	assert.Negative(t, cleared.MaxAge, "the refresh cookie is cleared")
	var user model.User
	require.NoError(t, model.DB.Where("oidc_id = ?", snapshottest.UserSub).First(&user).Error)
	assert.Greater(t, user.AuthVersion, int64(1), "the session ends at once")
	sessions, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
	require.NoError(t, err)
	assert.Zero(t, sessions)
}

func TestASuspendedPersonIsSignedOutOnTheNextRefresh(t *testing.T) {
	pool := esseraSignInSetup(t, "email_otp")
	signedIn := signInWithCode(t, "user-1@acme.example")
	require.True(t, signedIn.Success)

	suspended := snapshottest.Sample()
	suspended.Accounts.People[1].Status = "suspended"
	snapshot.Activate(snapshot.NewView(4, suspended)) // the version reaches this node before the projection

	refreshed := call(t, "/api/user/auth/refresh", `{}`, signedIn.cookie(service.RefreshCookieName))
	assert.Equal(t, http.StatusUnauthorized, refreshed.recorder.Code)
	assert.Len(t, pool.revoked, 1, "the Cognito sign-in is revoked")
	var rows int64
	require.NoError(t, model.DB.Model(&records.CognitoSession{}).Count(&rows).Error)
	assert.Zero(t, rows)
}

func TestSignOutRevokesTheCognitoSignIn(t *testing.T) {
	pool := esseraSignInSetup(t, "email_otp")
	signedIn := signInWithCode(t, "user-1@acme.example")
	require.True(t, signedIn.Success)
	var row records.CognitoSession
	require.NoError(t, model.DB.Where("session_id = ?", signedIn.Data.Session.SID).First(&row).Error)

	engine := gin.New()
	engine.POST("/api/user/auth/logout", AuthLogout)
	request := httptest.NewRequest(http.MethodPost, "/api/user/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+signedIn.Data.AccessToken)
	request.AddCookie(signedIn.cookie(service.RefreshCookieName))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, pool.revoked, 1)
	assert.Empty(t, pool.refresh, "the refresh token no longer works at Cognito")
	assert.ErrorIs(t, model.DB.Where("session_id = ?", row.SessionID).First(&records.CognitoSession{}).Error, gorm.ErrRecordNotFound)
}

func TestOnlyOperatorsKeepPasswordSignIn(t *testing.T) {
	esseraSignInSetup(t, "password")
	previous := common.PasswordLoginEnabled
	common.PasswordLoginEnabled = true
	t.Cleanup(func() { common.PasswordLoginEnabled = previous })
	hash, err := common.Password2Hash("operator-password")
	require.NoError(t, err)
	root := model.User{Username: "root", Password: hash, Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "root", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("oidc_id = ?", snapshottest.UserSub).Update("password", hash).Error)

	operator := call(t, "/api/user/login", `{"username":"root","password":"operator-password"}`)
	assert.True(t, operator.Success, operator.recorder.Body.String())
	person := call(t, "/api/user/login", `{"username":"user-1@acme.example","password":"operator-password"}`)
	assert.False(t, person.Success, "a person with a password set on the Gateway still signs in through Essera only")
	wrong := call(t, "/api/user/login", `{"username":"root","password":"wrong"}`)
	assert.Equal(t, wrong.Message, person.Message, "and is told what a wrong password is told")
}

func TestAnIDTokenTheGatewayCannotTrustSignsNobodyIn(t *testing.T) {
	pool := esseraSignInSetup(t, "email_otp")
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cases := map[string]func(jwt.MapClaims) *rsa.PrivateKey{
		"signed by another key":     func(jwt.MapClaims) *rsa.PrivateKey { return stranger },
		"for another app client":    func(c jwt.MapClaims) *rsa.PrivateKey { c["aud"] = "another-client"; return nil },
		"from another pool":         func(c jwt.MapClaims) *rsa.PrivateKey { c["iss"] = cognitoIssuer + "x"; return nil },
		"an access token":           func(c jwt.MapClaims) *rsa.PrivateKey { c["token_use"] = "access"; return nil },
		"expired":                   func(c jwt.MapClaims) *rsa.PrivateKey { c["exp"] = time.Now().Add(-time.Minute).Unix(); return nil },
		"naming a suspended person": func(c jwt.MapClaims) *rsa.PrivateKey { c["sub"] = snapshottest.SuspendedSub; return nil },
	}
	for name, forge := range cases {
		pool.forge = forge
		require.NoError(t, model.DB.Where("1 = 1").Delete(&records.SignInAttempt{}).Error)
		response := signInWithCode(t, "user-1@acme.example")
		assert.Equal(t, http.StatusUnauthorized, response.recorder.Code, name)
		assert.Equal(t, identity.ErrSignInFailed.Error(), response.Message, name)
		assert.Nil(t, response.cookie(service.RefreshCookieName), name)
	}
	var sessions int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessions).Error)
	assert.Zero(t, sessions)
}

func TestACodeStepThatRanOutSaysSoForEveryEmail(t *testing.T) {
	esseraSignInSetup(t, "email_otp")

	var answers []string
	for _, email := range []string{"user-1@acme.example", "nobody@acme.example"} {
		started := call(t, "/api/essera/sign-in", fmt.Sprintf(`{"email":%q}`, email))
		require.True(t, started.Success)
		assert.Contains(t, started.recorder.Body.String(), `"expires_in":180`)
		require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", identity.FlowPurpose).
			Update("expires_at", time.Now().Add(-time.Second)).Error)

		late := call(t, "/api/essera/sign-in/continue", fmt.Sprintf(`{"code":%q}`, goodCode), started.cookie("essera_sign_in"))

		assert.Equal(t, http.StatusUnauthorized, late.recorder.Code, email)
		assert.Equal(t, identity.ErrStepExpired.Error(), late.Message, email)
		answers = append(answers, late.recorder.Body.String())
	}
	assert.Equal(t, answers[0], answers[1], "an unknown email runs out the same way")
}

func TestCodeGuessingIsThrottledPerEmail(t *testing.T) {
	esseraSignInSetup(t, "email_otp")

	throttled := false
	for range identity.MaxAttemptsPerEmail {
		started := call(t, "/api/essera/sign-in", `{"email":"user-1@acme.example"}`)
		if started.recorder.Code == http.StatusTooManyRequests {
			throttled = true
			break
		}
		guess := call(t, "/api/essera/sign-in/continue", `{"code":"00000000"}`, started.cookie("essera_sign_in"))
		if guess.recorder.Code == http.StatusTooManyRequests {
			throttled = true
			break
		}
	}
	require.True(t, throttled, "each code sent and each guess counts against the email")
	right := call(t, "/api/essera/sign-in", `{"email":"user-1@acme.example"}`)
	assert.Equal(t, http.StatusTooManyRequests, right.recorder.Code)
	assert.Equal(t, identity.ErrThrottled.Error(), right.Message)
}

func TestAPersonCannotTakeAUsernameOrSetAGatewayPassword(t *testing.T) {
	esseraSignInSetup(t, "email_otp")
	var person model.User
	require.NoError(t, model.DB.Where("oidc_id = ?", snapshottest.UserSub).First(&person).Error)
	update := func(userID int, username, body string) *httptest.ResponseRecorder {
		engine := gin.New()
		engine.PUT("/api/user/self", func(c *gin.Context) { c.Set("id", userID); c.Set("username", username) }, UpdateSelf)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(body)))
		return recorder
	}

	for name, body := range map[string]string{
		"another username": `{"username":"owner@acme.example"}`,
		"a password":       `{"password":"Correct-Horse-9","original_password":""}`,
	} {
		recorder := update(person.Id, person.Username, body)
		assert.Equal(t, http.StatusConflict, recorder.Code, name)
	}
	unchanged := update(person.Id, person.Username, fmt.Sprintf(`{"username":%q,"display_name":"User One"}`, person.Username))
	assert.Equal(t, http.StatusOK, unchanged.Code, unchanged.Body.String())
	var after model.User
	require.NoError(t, model.DB.First(&after, person.Id).Error)
	assert.Equal(t, "user-1@acme.example", after.Username)
	assert.Equal(t, "User One", after.DisplayName, "the display name stays theirs")
}

func TestNobodySetsAnInterfaceLanguageOnAManagedGateway(t *testing.T) {
	esseraSignInSetup(t, "email_otp")
	var person model.User
	require.NoError(t, model.DB.Where("oidc_id = ?", snapshottest.UserSub).First(&person).Error)
	engine := gin.New()
	engine.PUT("/api/user/self", func(c *gin.Context) { c.Set("id", person.Id); c.Set("username", person.Username) }, UpdateSelf)
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(`{"language":"zhCN"}`)))

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "essera_not_available")
	var after model.User
	require.NoError(t, model.DB.First(&after, person.Id).Error)
	assert.Empty(t, after.GetSetting().Language, "the saved language is unchanged")
}

func TestAPersonSeesOnlyTheModelsRelayLetsThemCall(t *testing.T) {
	esseraSignInSetup(t, "email_otp")
	limited := snapshottest.Sample()
	limited.Accounts.EndUsers[0].Limits.Models = []string{"openai/gpt-4o", "acme-llm/acme-1"} // user-1, narrower than the customer
	view := snapshot.NewView(4, limited)
	snapshot.Activate(view)
	userIDs := map[string]int{}
	var people []model.User
	require.NoError(t, model.DB.Where("oidc_id <> ''").Find(&people).Error)
	for _, user := range people {
		userIDs[user.OidcId] = user.Id
	}
	modelsOf := func(userID int) []any {
		engine := gin.New()
		engine.GET("/api/user/models", func(c *gin.Context) { c.Set("id", userID) }, GetUserModels)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/user/models", nil))
		var body struct {
			Success bool  `json:"success"`
			Data    []any `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
		require.True(t, body.Success, recorder.Body.String())
		return body.Data
	}

	assert.Equal(t, []any{"claude-sonnet", "gpt-4o"}, modelsOf(userIDs[snapshottest.AdminSub]),
		"an admin: the customer's entitlements, without the model disabled in the catalog (gpt-old)")
	assert.Equal(t, []any{"gpt-4o"}, modelsOf(userIDs[snapshottest.UserSub]),
		"a user: their own limits within the customer's, never wider")
	assert.Empty(t, modelsOf(userIDs[snapshottest.SuspendedSub]), "a suspended person sees nothing")

	person := view.Person(snapshottest.UserPersonID)
	caller, failure := identity.Caller(view, person, nil)
	require.Nil(t, failure)
	request, denial := policy.Evaluate(caller, "user-1", "US", time.Now())
	require.Nil(t, denial)
	assert.Equal(t, request.AllowedModels, policy.Available(caller), "the list and relay cannot disagree")
}

func TestAManagedKeyCannotBeEnabledAgain(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	t.Setenv("ESSERA_MANAGED", "true")
	token := seedToken(t, db, 1, "revoked-in-the-console", "revoked1234token5678")
	require.NoError(t, db.Model(token).Update("status", keys.TokenStatusRevoked).Error)

	for name, target := range map[string]string{"status only": "/api/token/?status_only=true", "whole key": "/api/token/"} {
		body := map[string]any{"id": token.Id, "name": token.Name, "status": common.TokenStatusEnabled, "expired_time": -1, "unlimited_quota": true}
		ctx, recorder := newAuthenticatedContext(t, http.MethodPut, target, body, 1)
		UpdateToken(ctx)

		assert.Equal(t, http.StatusConflict, recorder.Code, name)
		assert.Contains(t, recorder.Body.String(), keys.ErrStateIsConsoles.Error(), name)
		var after model.Token
		require.NoError(t, db.First(&after, token.Id).Error)
		assert.Equal(t, keys.TokenStatusRevoked, after.Status, name)
	}
}

func TestAModelProbeSendsTheProviderAOneWordPromptForOneToken(t *testing.T) {
	ctx := probes.Context(t.Context(), probes.Request{Provider: "openai", Model: "gpt-4.1", Modality: "text"})
	request := probes.Shape(ctx, buildTestRequest("gpt-4.1", string(constant.EndpointTypeOpenAI), &model.Channel{}, true)).(*dto.GeneralOpenAIRequest)

	encoded := convertChatCompatibilityRequest(t, request, constant.ChannelTypeOpenAI, nil)

	assert.JSONEq(t, `{"model":"gpt-4.1","messages":[{"role":"user","content":"Hi"}],"stream":true,
		"stream_options":{"include_usage":true},"max_tokens":1}`, string(encoded))
}
