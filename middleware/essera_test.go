// Essera's own test of its TokenAuth hook (CHANGES-ESSERA.md).
package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheVideoRoutesServeBedrockVideoModelsAndOnlyTheCustomersOwnTasks(t *testing.T) {
	recordstest.UseDatabase(t)
	require.NoError(t, model.DB.Create(&records.Task{TaskID: "task_mine", CustomerID: "customer-1"}).Error)
	request := func(method, path, body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	assert.True(t, esseraVideoAllowed(request(http.MethodPost, "/v1/video/generations", `{"model":"luma.ray-v2:0","prompt":"p"}`), "customer-1"))
	refused := request(http.MethodPost, "/v1/videos", `{"model":"amazon.nova-lite-v1:0","prompt":"p"}`)
	assert.False(t, esseraVideoAllowed(refused, "customer-1"), "a model the Console cannot account for as a video task")
	assert.Equal(t, http.StatusNotFound, refused.Writer.Status())

	assert.True(t, esseraVideoAllowed(request(http.MethodGet, "/v1/videos/task_mine/content", ""), "customer-1"))
	for _, path := range []string{"/v1/video/generations/task_mine", "/v1/videos/task_unknown"} {
		other := request(http.MethodGet, path, "")
		assert.False(t, esseraVideoAllowed(other, "customer-2"), "every customer runs as one new-api user: %s", path)
		assert.Equal(t, http.StatusNotFound, other.Writer.Status(), path)
	}
	assert.True(t, esseraVideoAllowed(request(http.MethodPost, "/v1/chat/completions", `{}`), "customer-2"), "other routes are not video routes")
}

func TestAVideoModelOutsideTheEntitlementsIsRefusedBeforeAnyProviderCall(t *testing.T) {
	require.NoError(t, i18n.Init())
	providerCalled := false
	engine := gin.New()
	engine.POST("/v1/video/generations", func(c *gin.Context) {
		c.Set(usage.ContextKey, &policy.Request{AllowedModels: map[string][]string{"amazon.nova-reel-v1:1": {"essera/bedrock"}}})
		esseraLimitModels(c)
	}, Distribute(), func(c *gin.Context) { providerCalled = true })

	recorder := httptest.NewRecorder()
	submit := httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{"model":"luma.ray-v2:0","prompt":"a fox"}`))
	submit.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, submit)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.False(t, providerCalled, "Bedrock is never called")
}

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
