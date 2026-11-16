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
		{http.MethodPost, "/v1/video/generations", http.StatusNotFound},
		{http.MethodPost, "/v1/tasks", http.StatusNotFound},
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

func TestCustomerPathAllowed(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/models", "/v1/models/gpt-4o", "/v1/audio/speech", "/v1beta/models/gemini:generateContent"} {
		assert.True(t, CustomerPathAllowed(path), path)
	}
	for _, path := range []string{"/v1/video/generations", "/v1/tasks", "/api/token/", "/v1/chat/completionsx", "/pg/chat/completions"} {
		assert.False(t, CustomerPathAllowed(path), path)
	}
}
