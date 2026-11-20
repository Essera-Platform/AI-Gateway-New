// Essera fork: connects new-api's channel test to the Console's connection tests (M02-C07),
// and new-api's sign-in and token handlers to the Console's people and keys; serves the API's
// OpenAPI document.
package controller

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/apidocs"
	"github.com/QuantumNous/new-api/essera/bootstrap"
	"github.com/QuantumNous/new-api/essera/bridge"
	"github.com/QuantumNous/new-api/essera/identity"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// esseraSignInCookie holds the opaque id of a sign-in between two steps. The Cognito session it
// stands for stays on the server, so the page never holds anything it could replay.
const esseraSignInCookie = "essera_sign_in"

type esseraSignInRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

// EsseraSignIn starts the sign-in of a person on a managed gateway: their email, and their
// password unless the gateway signs in with emailed codes.
func EsseraSignIn(c *gin.Context) {
	var request esseraSignInRequest
	if !settings.Managed() || common.DecodeJson(c.Request.Body, &request) != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Not found"})
		return
	}
	outcome, err := identity.Start(c.Request.Context(), request.Email, request.Password, c.ClientIP(), time.Now())
	writeEsseraSignIn(c, outcome, err)
}

// EsseraSignInContinue answers the step the sign-in waits for: the emailed code, or the new
// password a first sign-in needs.
func EsseraSignInContinue(c *gin.Context) {
	var request esseraSignInRequest
	if !settings.Managed() || common.DecodeJson(c.Request.Body, &request) != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Not found"})
		return
	}
	flow, _ := c.Cookie(esseraSignInCookie)
	response := request.Code
	if request.NewPassword != "" {
		response = request.NewPassword
	}
	outcome, err := identity.Continue(c.Request.Context(), flow, response, c.ClientIP(), time.Now())
	writeEsseraSignIn(c, outcome, err)
}

func writeEsseraSignIn(c *gin.Context, outcome *identity.Outcome, err error) {
	setAuthNoStore(c)
	if err != nil {
		setEsseraSignInCookie(c, "", -1)
		status := http.StatusUnauthorized
		switch {
		case errors.Is(err, identity.ErrThrottled):
			status = http.StatusTooManyRequests
		case errors.Is(err, identity.ErrUnavailable):
			status = http.StatusServiceUnavailable
		case errors.Is(err, identity.ErrStepExpired):
		case !errors.Is(err, identity.ErrSignInFailed):
			common.SysError(common.LogText("essera: sign-in: %s", err.Error()))
			status, err = http.StatusServiceUnavailable, identity.ErrUnavailable
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	if outcome.User == nil {
		setEsseraSignInCookie(c, outcome.Flow, int(identity.FlowTTL/time.Second))
		c.JSON(http.StatusOK, gin.H{"success": outcome.Problem == "", "message": outcome.Problem, "data": gin.H{"next": outcome.Next, "expires_in": int(identity.FlowTTL / time.Second)}})
		return
	}
	setEsseraSignInCookie(c, "", -1)
	c.Set("login_method", identity.LoginMethod)
	if esseraLoginRefused(c, outcome.User) {
		return
	}
	// A person's second factors are Cognito's; new-api's login challenge is not asked.
	bundle, err := service.CreateLoginSessionAtAuthVersion(outcome.User.Id, outcome.User.AuthVersion, identity.LoginMethod, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	if err := identity.Bind(bundle.Session.SID, outcome.User.Id, outcome.Sub, outcome.RefreshToken); err != nil {
		_, _ = model.RevokeUserSession(outcome.User.Id, bundle.Session.SID, "essera_sign_in_failed")
		writeAuthSessionError(c, err)
		return
	}
	currentUser, err := model.GetSelfUserById(outcome.User.Id)
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	writeLoginResponse(c, currentUser, bundle)
}

func setEsseraSignInCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     esseraSignInCookie,
		Value:    value,
		Path:     "/api/essera/sign-in",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

// esseraRefreshRefused renews the Cognito sign-in behind a refreshed session and ends the
// user's sessions when it is over.
func esseraRefreshRefused(c *gin.Context, sessionID string, userID int) bool {
	err := identity.Refresh(c.Request.Context(), sessionID, userID, time.Now())
	if err == nil {
		return false
	}
	if errors.Is(err, identity.ErrSessionEnded) {
		service.ClearRefreshCookie(c)
		writeAuthSessionError(c, service.ErrLoginSessionRevoked)
		return true
	}
	writeAuthSessionError(c, err)
	return true
}

// esseraLoginRefused applies who may start a session on a managed gateway, whatever the path
// that authenticated them, and answers as a wrong password does when they may not.
func esseraLoginRefused(c *gin.Context, user *model.User) bool {
	allowed, err := identity.LoginAllowed(user.Id, user.Role, loginMethodFromContext(c))
	if err != nil {
		writeSecurityOperationError(c, err)
		return true
	}
	if !allowed {
		common.SysLog(common.LogText("essera: %s sign-in refused for user %d from %s", loginMethodFromContext(c), user.Id, c.ClientIP()))
		common.ApiErrorT(c, "Username or password is incorrect, or user has been banned")
		return true
	}
	return false
}

// esseraSelfEditRefused refuses a person's change of their username or a Gateway password on
// a managed gateway: the username is their email from the Console, and they sign in through
// Cognito only. Their language, sidebar and display name stay theirs to change.
func esseraSelfEditRefused(c *gin.Context, requestData map[string]any, passwordRequested bool) bool {
	if !settings.Managed() {
		return false
	}
	username, renaming := requestData["username"].(string)
	if !passwordRequested && (!renaming || username == "" || username == c.GetString("username")) {
		return false
	}
	person, err := identity.IsPerson(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return true
	}
	if !person {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{"success": false, "message": "This is managed in the Essera Operations Console."})
	return true
}

// esseraPersonModels answers a person's own model list on a managed gateway with the models
// relay lets them call (policy.Available); nothing while they are not active. handled is false
// for an operator, whose list stays new-api's.
func esseraPersonModels(c *gin.Context) (handled bool) {
	if !settings.Managed() {
		return false
	}
	userID := c.GetInt("id")
	isPerson, err := identity.IsPerson(userID)
	if err != nil {
		common.ApiError(c, err)
		return true
	}
	if !isPerson {
		return false
	}
	common.ApiSuccess(c, esseraAvailableModels(userID))
	return true
}

// esseraAvailableModels are the models relay lets a person call (policy.Available), sorted;
// none while they are not active or for anyone who is not a person.
func esseraAvailableModels(userID int) []string {
	models := []string{}
	view := snapshot.Current()
	if person, err := identity.ActivePerson(userID, view); err == nil {
		if caller, failure := identity.Caller(view, person, nil); failure == nil {
			models = slices.Sorted(maps.Keys(policy.Available(caller)))
		}
	}
	return models
}

// EsseraOpenAPI serves the OpenAPI document of the customer API to a signed-in user: the
// address people call the Gateway at, and examples with models the reader may call.
func EsseraOpenAPI(c *gin.Context) {
	address := settings.ServerAddress(system_setting.ServerAddress)
	if address == "" {
		address = "https://" + c.Request.Host
	}
	var available []string
	if settings.Managed() {
		available = esseraAvailableModels(c.GetInt("id"))
	}
	spec, err := apidocs.Spec(address, available)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", spec)
}

// addEsseraKey creates a person's key on a managed gateway. The key is in this answer and
// nowhere else.
func addEsseraKey(c *gin.Context, name string, expiredTime int64, params model.AuditFields) {
	created, err := keys.Create(c.GetInt("id"), name, expiredTime, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params["id"], params["key_id"] = created.TokenID, created.KeyID
	common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true)
	setAuthNoStore(c)
	common.ApiSuccess(c, created)
}

func init() {
	bridge.RunChannelTest = runEsseraChannelTest
}

// runEsseraChannelTest tests a provider's channel as the Essera service user: one minimal
// request with the configured key, reporting success, latency and the provider's error.
func runEsseraChannelTest(ctx context.Context, channel *model.Channel) bridge.TestResult {
	var user model.User
	if err := model.DB.Select("id").Where("username = ?", bootstrap.ServiceUsername).First(&user).Error; err != nil {
		return bridge.TestResult{Message: "The Gateway's service user is missing."}
	}
	started := time.Now()
	result := testChannel(ctx, channel, user.Id, "", "", false)
	latency := time.Since(started)
	switch {
	case result.localErr != nil:
		return bridge.TestResult{Message: result.localErr.Error()}
	case result.newAPIError != nil:
		return bridge.TestResult{Latency: latency, Message: result.newAPIError.Error()}
	}
	go channel.UpdateResponseTime(latency.Milliseconds())
	return bridge.TestResult{Succeeded: true, Latency: latency}
}
