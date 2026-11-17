// Essera fork: customer authentication and policy for a Gateway managed by the Essera
// Operations Console. TokenAuth calls these; everything else of the request is new-api's.
package middleware

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/bootstrap"
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

// esseraAuthenticate handles a presented key that is a customer's access token. On success it
// returns the service token's key, so new-api serves the request as the Essera service user;
// on refusal it aborts the request. handled is false for an ordinary new-api key.
func esseraAuthenticate(c *gin.Context, presented string) (serviceKey string, handled bool) {
	if !settings.Managed() {
		return "", false
	}
	if !auth.LooksLikeJWT(presented) {
		if !managed.RelayPath(c.Request.URL.Path) {
			return "", false
		}
		// Every request a provider serves is a Console customer's: a new-api key would be served
		// without the Console's policy, limits or usage.
		stats.Denial(time.Now(), "authentication", http.StatusUnauthorized)
		abortWithOpenAiMessage(c, http.StatusUnauthorized, "Use the credential issued in the Essera Operations Console.", types.ErrorCode("essera_unauthenticated"))
		return "", true
	}
	now := time.Now()
	if !managed.CustomerRequestAllowed(c.Request.Method, c.Request.URL.Path) {
		abortWithOpenAiMessage(c, http.StatusNotFound, "This endpoint is not available to customer credentials.", types.ErrorCode("essera_not_available"))
		return "", true
	}
	caller, failure := auth.Authenticate(c.Request.Context(), presented, snapshot.Current(), now)
	if failure != nil {
		status := http.StatusUnauthorized
		if failure.Code == "essera_unavailable" {
			status = http.StatusServiceUnavailable
		}
		stats.Denial(now, "authentication", status)
		abortWithOpenAiMessage(c, status, failure.Message, types.ErrorCode(failure.Code))
		return "", true
	}
	request, denial := policy.Evaluate(caller, c.GetHeader("X-Essera-End-User"), c.GetHeader("CF-IPCountry"), now)
	if denial != nil {
		stats.Denial(now, denial.Reason, denial.Status)
		for name, value := range denial.Headers {
			c.Header(name, value)
		}
		abortWithOpenAiMessage(c, denial.Status, denial.Message, types.ErrorCode(denial.Code))
		return "", true
	}
	for name, value := range request.Headers {
		c.Header(name, value)
	}
	c.Set(usage.ContextKey, request)
	service.GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterEsseraProviders, EsseraTagsByModel: request.AllowedModels})
	return bootstrap.ServiceTokenKey(), true
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
