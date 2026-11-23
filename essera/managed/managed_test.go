package managed

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGuard(t *testing.T) {
	t.Setenv("ESSERA_MANAGED", "true")
	engine := gin.New()
	engine.Use(Guard())
	engine.Any("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	cases := []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/v1/chat/completions", http.StatusOK},
		{http.MethodPost, "/pg/chat/completions", http.StatusOK},
		{http.MethodPost, "/mj/submit/imagine", http.StatusNotFound},
		{http.MethodPost, "/fast/mj/submit/imagine", http.StatusNotFound},
		{http.MethodPost, "/v1/video/generations", http.StatusNotFound},
		{http.MethodPost, "/v1/tasks", http.StatusNotFound},
		{http.MethodGet, "/api/channel/", http.StatusOK},
		{http.MethodPost, "/api/channel/", http.StatusConflict},
		{http.MethodPut, "/api/channel/", http.StatusConflict},
		{http.MethodPost, "/api/token/", http.StatusOK},
		{http.MethodDelete, "/api/token/12/", http.StatusOK},
		{http.MethodPost, "/api/token/batch", http.StatusOK},
		{http.MethodPut, "/api/token/", http.StatusConflict},
		{http.MethodPost, "/api/token/12/key", http.StatusConflict},
		{http.MethodPost, "/api/token/batch/keys", http.StatusConflict},
		{http.MethodPut, "/api/option/", http.StatusConflict},
		{http.MethodPut, "/api/option/request_policy", http.StatusOK},
		{http.MethodPost, "/api/user/", http.StatusConflict},
		{http.MethodDelete, "/api/user/12", http.StatusConflict},
		{http.MethodPost, "/api/user/register", http.StatusConflict},
		{http.MethodPost, "/api/user/login", http.StatusOK},
		{http.MethodGet, "/api/oauth/oidc", http.StatusNotFound},
		{http.MethodPost, "/api/oauth/state", http.StatusNotFound},
		{http.MethodPost, "/api/essera/sign-in", http.StatusOK},
		{http.MethodPost, "/api/user/logout", http.StatusOK},
		{http.MethodPut, "/api/user/self", http.StatusOK},
		{http.MethodDelete, "/api/user/self", http.StatusConflict},
		{http.MethodGet, "/api/status", http.StatusOK},
		{http.MethodGet, "/api/pricing", http.StatusOK},
		{http.MethodGet, "/api/user/sessions", http.StatusOK},
		{http.MethodGet, "/api/log/self", http.StatusOK},
		{http.MethodGet, "/api/subscription/self", http.StatusNotFound},
		{http.MethodPost, "/api/subscription/admin/plans", http.StatusNotFound},
		{http.MethodGet, "/api/redemption/", http.StatusNotFound},
		{http.MethodGet, "/api/user/topup/info", http.StatusNotFound},
		{http.MethodPost, "/api/user/topup/complete", http.StatusNotFound},
		{http.MethodPost, "/api/user/stripe/pay", http.StatusNotFound},
		{http.MethodPost, "/api/stripe/webhook", http.StatusNotFound},
		{http.MethodPost, "/api/waffo-pancake/webhook/test", http.StatusNotFound},
		{http.MethodGet, "/api/user/epay/notify", http.StatusNotFound},
		{http.MethodGet, "/api/user/aff", http.StatusNotFound},
		{http.MethodPost, "/api/user/aff_transfer", http.StatusNotFound},
		{http.MethodPost, "/api/user/checkin", http.StatusNotFound},
		{http.MethodGet, "/api/reset_password", http.StatusNotFound},
		{http.MethodPost, "/api/user/reset", http.StatusNotFound},
		{http.MethodGet, "/api/verification", http.StatusNotFound},
		{http.MethodPost, "/api/oauth/email/bind/start", http.StatusNotFound},
		{http.MethodGet, "/api/oauth/github", http.StatusNotFound},
		{http.MethodGet, "/api/about", http.StatusNotFound},
		{http.MethodGet, "/api/notice", http.StatusNotFound},
		{http.MethodGet, "/api/uptime/status", http.StatusNotFound},
		{http.MethodGet, "/api/rankings", http.StatusNotFound},
		{http.MethodGet, "/api/ratio_config", http.StatusNotFound},
		{http.MethodGet, "/api/mj/self", http.StatusNotFound},
		{http.MethodGet, "/api/task/self", http.StatusNotFound},
		{http.MethodGet, "/api/task/abc/artifacts", http.StatusNotFound},
		{http.MethodGet, "/api/usage/token/", http.StatusNotFound},
		{http.MethodGet, "/api/log/token", http.StatusNotFound},
		{http.MethodPost, "/suno/submit/music", http.StatusNotFound},
		{http.MethodGet, "/v1/videos/abc/content", http.StatusNotFound},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		assert.Equal(t, tc.status, recorder.Code, "%s %s", tc.method, tc.path)
	}
}

func TestGuardIsOffUnlessManaged(t *testing.T) {
	engine := gin.New()
	engine.Use(Guard())
	engine.Any("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/channel/", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestCustomerRequestAllowed(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/embeddings", "/v1/embeddings/", "/v1/embeddings/tasks"} {
		assert.True(t, CustomerRequestAllowed(http.MethodPost, path), path)
	}
	for _, path := range []string{"/v1/models", "/v1/models/eu.amazon.nova-lite-v1:0", "/v1/embeddings/tasks/embtask-1"} {
		assert.True(t, CustomerRequestAllowed(http.MethodGet, path), path)
	}
	for _, path := range []string{"/v1/embeddings/tasks", "/v1/embeddings/tasks/embtask-1/content", "/v1/embeddings"} {
		assert.False(t, CustomerRequestAllowed(http.MethodGet, path), "only one task is read: %s", path)
	}
	assert.False(t, CustomerRequestAllowed(http.MethodPost, "/v1/embeddings/tasks/embtask-1"))
	closed := map[string]string{
		"/v1/completions":                       "legacy completions: the Bedrock adaptor serves none",
		"/v1/images/generations":                "no image model in the Console's region",
		"/v1/images/edits":                      "no image model in the Console's region",
		"/v1/audio/speech":                      "no speech model in the Console's region",
		"/v1/audio/transcriptions":              "audio goes in as chat input, not here",
		"/v1/rerank":                            "no rerank model in the Console's region",
		"/v1/moderations":                       "Bedrock has no moderations",
		"/v1/responses/compact":                 "a compaction, not a measured response",
		"/v1/models/gemini:generateContent":     "Gemini's format is not converted for Bedrock",
		"/v1beta/models/gemini:generateContent": "Gemini's format is not converted for Bedrock",
		"/v1/video/generations":                 "a task without a usage event",
		"/v1/tasks":                             "a task without a usage event",
		"/v1/chat/completionsx":                 "not a relay route",
		"/pg/chat/completions":                  "the playground runs as the signed-in person",
		"/api/token/":                           "the admin UI",
	}
	for path, why := range closed {
		assert.False(t, CustomerRequestAllowed(http.MethodPost, path), "%s: %s", path, why)
	}
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1/responses"), "a GET opens a WebSocket whose calls are not accounted for")
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1/realtime"), "realtime: no such model in the Console's region")
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1beta/models"), "Gemini's model list")
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1/dashboard/billing/usage"), "new-api's balance; the Console bills")
	assert.False(t, CustomerRequestAllowed(http.MethodDelete, "/v1/models/gpt-4o"))
}

func TestPersonRouteRefused(t *testing.T) {
	t.Setenv("ESSERA_MANAGED", "true")
	closed := []string{
		"/api/user/access_tokens", "/api/user/access_tokens/catalog", "/api/user/access_tokens/3",
		"/api/user/passkey", "/api/user/passkey/register/begin", "/api/user/2fa/status", "/api/user/2fa/setup",
		"/api/user/oauth/bindings", "/api/user/oauth/bindings/2", "/api/user/setting", "/api/verify", "/api/verify/methods",
	}
	open := []string{"/api/user/self", "/api/user/sessions", "/api/user/sessions/revoke-others", "/api/user/models", "/api/token/", "/api/log/self", "/api/essera/people"}
	refused := func(role int, path string) (bool, int) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		return PersonRouteRefused(c, role), c.Writer.Status()
	}
	for _, path := range closed {
		was, status := refused(common.RoleCommonUser, path)
		assert.True(t, was, path)
		assert.Equal(t, http.StatusNotFound, status, path)
		was, _ = refused(common.RoleRootUser, path)
		assert.False(t, was, "an operator keeps %s", path)
	}
	for _, path := range open {
		was, _ := refused(common.RoleCommonUser, path)
		assert.False(t, was, path)
	}

	t.Setenv("ESSERA_MANAGED", "false")
	was, _ := refused(common.RoleCommonUser, "/api/user/passkey")
	assert.False(t, was, "an unmanaged gateway is upstream's")
}

func TestTrimStatus(t *testing.T) {
	status := func() map[string]any {
		return map[string]any{
			"chats": []map[string]string{{"Cherry Studio": "cherrystudio://"}}, "docs_link": "https://docs.example",
			"announcements_enabled": true, "announcements": []any{"hello"}, "uptime_kuma_enabled": true,
			"api_info_enabled": true, "api_info": []any{"x"}, "faq_enabled": true, "faq": []any{"y"},
			"github_oauth": true, "custom_oauth_providers": []any{"gitea"}, "system_name": "Essera AI Gateway",
		}
	}
	unmanaged := status()
	TrimStatus(unmanaged)
	assert.Equal(t, status(), unmanaged, "an unmanaged gateway is upstream's")

	t.Setenv("ESSERA_MANAGED", "true")
	managed := status()
	TrimStatus(managed)
	assert.Empty(t, managed["chats"])
	assert.Empty(t, managed["docs_link"])
	for _, key := range []string{"announcements_enabled", "uptime_kuma_enabled", "api_info_enabled", "faq_enabled", "github_oauth", "register_enabled", "checkin_enabled"} {
		assert.Equal(t, false, managed[key], key)
	}
	assert.NotContains(t, managed, "announcements")
	assert.NotContains(t, managed, "api_info")
	assert.NotContains(t, managed, "faq")
	assert.NotContains(t, managed, "custom_oauth_providers")
	assert.Equal(t, "Essera AI Gateway", managed["system_name"], "the rest stays")
}

func TestPluginAndSignInProviderWritesAreTheConsoles(t *testing.T) {
	for _, path := range []string{"/api/plugin/task/upload", "/api/request_plugin/instances", "/api/custom-oauth-provider/"} {
		assert.True(t, adminWrites.MatchString(path), path)
	}
}
