package managed

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
		{http.MethodPost, "/pg/chat/completions", http.StatusNotFound},
		{http.MethodPost, "/mj/submit/imagine", http.StatusNotFound},
		{http.MethodPost, "/fast/mj/submit/imagine", http.StatusNotFound},
		{http.MethodPost, "/v1/video/generations", http.StatusOK},
		{http.MethodGet, "/v1/video/generations/task_1", http.StatusOK},
		{http.MethodPost, "/v1/videos", http.StatusOK},
		{http.MethodGet, "/v1/videos/task_1/content", http.StatusOK},
		{http.MethodPost, "/v1/videos/task_1/remix", http.StatusNotFound},
		{http.MethodGet, "/v1/video/task_1", http.StatusNotFound},
		{http.MethodPost, "/v1/tasks", http.StatusNotFound},
		{http.MethodPost, "/v1/tasks/kling", http.StatusNotFound},
		{http.MethodGet, "/api/channel/", http.StatusOK},
		{http.MethodPost, "/api/channel/", http.StatusConflict},
		{http.MethodPut, "/api/channel/", http.StatusConflict},
		{http.MethodPost, "/api/token/", http.StatusConflict},
		{http.MethodPut, "/api/option/", http.StatusConflict},
		{http.MethodPut, "/api/option/request_policy", http.StatusOK},
		{http.MethodPost, "/api/user/", http.StatusConflict},
		{http.MethodDelete, "/api/user/12", http.StatusConflict},
		{http.MethodPost, "/api/user/register", http.StatusConflict},
		{http.MethodPost, "/api/user/login", http.StatusOK},
		{http.MethodPost, "/api/user/logout", http.StatusOK},
		{http.MethodPut, "/api/user/self", http.StatusOK},
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
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/audio/speech", "/v1beta/models/gemini:generateContent"} {
		assert.True(t, CustomerRequestAllowed(http.MethodPost, path), path)
	}
	for _, path := range []string{"/v1/models", "/v1/models/gpt-4o", "/v1beta/models"} {
		assert.True(t, CustomerRequestAllowed(http.MethodGet, path), path)
	}
	for _, path := range []string{"/v1/video/generations", "/v1/videos"} {
		assert.True(t, CustomerRequestAllowed(http.MethodPost, path), "a Bedrock video, reported when it ends: %s", path)
	}
	for _, path := range []string{"/v1/video/generations/task_1", "/v1/videos/task_1", "/v1/videos/task_1/content"} {
		assert.True(t, CustomerRequestAllowed(http.MethodGet, path), path)
	}
	for _, path := range []string{"/v1/videos/task_1/remix", "/v1/tasks", "/v1/tasks/kling", "/api/token/", "/v1/chat/completionsx", "/pg/chat/completions"} {
		assert.False(t, CustomerRequestAllowed(http.MethodPost, path), path)
	}
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1/tasks/task_1"), "the plugins' task API reads no Bedrock video")
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1/responses"), "a GET opens a WebSocket whose calls are not accounted for")
	assert.False(t, CustomerRequestAllowed(http.MethodGet, "/v1/chat/completions"))
	assert.False(t, CustomerRequestAllowed(http.MethodPost, "/v1beta/models/veo:predictLongRunning"), "a task without a usage event")
}

func TestPluginAndSignInProviderWritesAreTheConsoles(t *testing.T) {
	for _, path := range []string{"/api/plugin/task/upload", "/api/request_plugin/instances", "/api/custom-oauth-provider/"} {
		assert.True(t, adminWrites.MatchString(path), path)
	}
}

func TestVideoTaskRead(t *testing.T) {
	for path, want := range map[string]string{"/v1/video/generations/task_a": "task_a", "/v1/videos/task_b": "task_b", "/v1/videos/task_c/content": "task_c"} {
		taskID, ok := VideoTaskRead(http.MethodGet, path)
		assert.True(t, ok, path)
		assert.Equal(t, want, taskID, path)
	}
	_, ok := VideoTaskRead(http.MethodPost, "/v1/videos/task_b")
	assert.False(t, ok)
	_, ok = VideoTaskRead(http.MethodGet, "/v1/videos")
	assert.False(t, ok)
}
