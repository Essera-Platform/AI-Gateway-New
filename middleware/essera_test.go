// Essera's own test of its TokenAuth hook (CHANGES-ESSERA.md).
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
