// Essera's own test of its TokenAuth hook (CHANGES-ESSERA.md).
package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestManagedRelayRefusesKeysThatAreNotConsoleCredentials(t *testing.T) {
	t.Setenv("ESSERA_MANAGED", "true")
	for path, refused := range map[string]bool{"/v1/chat/completions": true, "/v1beta/models/x:generateContent": true, "/api/usage/token": false} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)

		_, handled := esseraAuthenticate(c, "sk-abc123")

		assert.Equal(t, refused, handled, path)
		assert.Equal(t, refused, c.IsAborted(), path)
	}

	t.Setenv("ESSERA_MANAGED", "false")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, handled := esseraAuthenticate(c, "sk-abc123")
	assert.False(t, handled, "an unmanaged gateway is upstream's")
}

func TestRefusalsAfterThePolicyAreCountedOnceAndOnlyForCustomers(t *testing.T) {
	var counted []string
	countDenial = func(_ time.Time, reason string, status int) {
		counted = append(counted, fmt.Sprintf("%s %d", reason, status))
	}
	t.Cleanup(func() { countDenial = stats.Denial })
	refuse := func(admitted bool, status int) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		if admitted {
			c.Set(usage.ContextKey, &policy.Request{})
		}
		abortWithOpenAiMessage(c, status, "refused")
		abortWithOpenAiMessage(c, status, "refused again")
	}

	refuse(true, http.StatusForbidden)
	refuse(true, http.StatusServiceUnavailable)
	refuse(true, http.StatusBadRequest)
	refuse(false, http.StatusForbidden)

	assert.Equal(t, []string{"model 403", "no_channel 503", "invalid_request 400"}, counted,
		"a request the policy refused is counted by the policy, and an aborted one only once")
}
