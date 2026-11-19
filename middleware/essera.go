// Essera fork: customer authentication and policy for a Gateway managed by the Essera
// Operations Console. TokenAuth and the playground route call these; everything else of the
// request is new-api's.
package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/bootstrap"
	"github.com/QuantumNous/new-api/essera/identity"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/managed"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// esseraAuthenticate handles the key of a relay request: a customer's access token, or an API
// key a person created and the Console published. On success it returns the service token's
// key, so new-api serves the request as the Essera service user; on refusal it aborts the
// request. handled is false for an ordinary new-api key outside the relay routes.
func esseraAuthenticate(c *gin.Context, presented string) (serviceKey string, handled bool) {
	if !settings.Managed() {
		return "", false
	}
	jwt := auth.LooksLikeJWT(presented)
	if !jwt && !managed.RelayPath(c.Request.URL.Path) {
		return "", false
	}
	now := time.Now()
	if !managed.CustomerRequestAllowed(c.Request.Method, c.Request.URL.Path) {
		abortWithOpenAiMessage(c, http.StatusNotFound, "This endpoint is not available to customer credentials.", types.ErrorCode("essera_not_available"))
		return "", true
	}
	var caller *auth.Caller
	var failure *auth.Failure
	if jwt {
		caller, failure = auth.Authenticate(c.Request.Context(), presented, snapshot.Current(), now)
	} else {
		// Every request a provider serves is a Console customer's: a new-api key, an operator's
		// among them, would be served without the Console's policy, limits or usage.
		caller, failure = keys.Authenticate(presented, snapshot.Current(), now)
	}
	if failure != nil {
		status := http.StatusUnauthorized
		if failure.Code == "essera_unavailable" {
			status = http.StatusServiceUnavailable
		}
		stats.Denial(now, "authentication", status)
		abortWithOpenAiMessage(c, status, failure.Message, types.ErrorCode(failure.Code))
		return "", true
	}
	if !esseraAdmit(c, caller, now) {
		return "", true
	}
	return bootstrap.ServiceTokenKey(), true
}

// esseraAdmit applies the policy to an authenticated caller and readies an admitted request
// for routing and usage. A person's end user is their own, never one the request names.
func esseraAdmit(c *gin.Context, caller *auth.Caller, now time.Time) bool {
	endUserID, personal := caller.EndUserID()
	if !personal {
		endUserID = c.GetHeader("X-Essera-End-User")
	}
	request, denial := policy.Evaluate(caller, endUserID, c.GetHeader("CF-IPCountry"), now)
	if denial != nil {
		stats.Denial(now, denial.Reason, denial.Status)
		for name, value := range denial.Headers {
			c.Header(name, value)
		}
		abortWithOpenAiMessage(c, denial.Status, denial.Message, types.ErrorCode(denial.Code))
		return false
	}
	for name, value := range request.Headers {
		c.Header(name, value)
	}
	c.Set(usage.ContextKey, request)
	service.GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterEsseraProviders, EsseraTagsByModel: request.AllowedModels})
	return true
}

// EsseraPlayground runs a playground request of a managed gateway as the signed-in person:
// their customer, the same policy, limits and usage as their API calls. Anyone who is not an
// active person, operators included, is refused: their requests would bypass the Console.
func EsseraPlayground() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settings.Managed() {
			c.Next()
			return
		}
		now := time.Now()
		view := snapshot.Current()
		person, err := identity.ActivePerson(c.GetInt("id"), view)
		if err != nil {
			var refusal *identity.Refusal
			status, message := http.StatusServiceUnavailable, "The gateway cannot check this account right now."
			if errors.As(err, &refusal) {
				status, message = http.StatusForbidden, "The playground is open to the people of a Console customer."
			}
			stats.Denial(now, "authentication", status)
			abortWithOpenAiMessage(c, status, message, types.ErrorCode("essera_unauthenticated"))
			return
		}
		caller, failure := identity.Caller(view, person, nil)
		if failure != nil {
			stats.Denial(now, "authentication", http.StatusForbidden)
			abortWithOpenAiMessage(c, http.StatusForbidden, failure.Message, types.ErrorCode(failure.Code))
			return
		}
		if !esseraAdmit(c, caller, now) {
			return
		}
		esseraLimitModels(c)
		c.Next()
	}
}

// esseraLimitModels narrows the service token's models to the customer's, once new-api has
// set up the token, so a model outside the entitlements is refused before routing.
func esseraLimitModels(c *gin.Context) {
	value, ok := c.Get(usage.ContextKey)
	if !ok {
		return
	}
	request := value.(*policy.Request)
	allowed := make(map[string]bool, len(request.AllowedModels))
	for model := range request.AllowedModels {
		allowed[model] = true
	}
	c.Set("token_model_limit_enabled", true)
	c.Set("token_model_limit", allowed)
}

var countDenial = stats.Denial

// esseraCountRefusal counts a customer's request that new-api refuses after the Essera policy
// let it through: a model outside the customer's entitlements, no channel to serve it, a
// request it cannot read. Such a request never reaches relay, so nothing else counts it.
func esseraCountRefusal(c *gin.Context, status int) {
	if _, admitted := c.Get(usage.ContextKey); !admitted || c.IsAborted() {
		return
	}
	countDenial(time.Now(), refusalReason(status), status)
}

func refusalReason(status int) string {
	switch status {
	case http.StatusForbidden:
		return "model"
	case http.StatusTooManyRequests:
		return "rate_limit"
	case http.StatusServiceUnavailable:
		return "no_channel"
	}
	return "invalid_request"
}
