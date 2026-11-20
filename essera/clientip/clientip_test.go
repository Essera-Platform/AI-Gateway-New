package clientip_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/essera/clientip"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// server is the Gateway's engine as main.go builds it: TRUSTED_PROXIES applied, then wrapped.
func server(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NoError(t, middleware.ConfigureTrustedProxies(engine))
	engine.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	handler, err := clientip.Wrap(engine)
	require.NoError(t, err)
	return handler
}

func clientIP(handler http.Handler, peer string, headers map[string][]string) string {
	request := httptest.NewRequest(http.MethodGet, "/ip", nil)
	request.RemoteAddr = peer
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Body.String()
}

func TestTheClientIsTheEdgesHeaderOnlyFromATrustedProxy(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "127.0.0.0/8,::1,10.10.0.0/24,10.10.1.0/24,10.10.10.0/24")
	t.Setenv("ESSERA_CLIENT_IP_HEADER", "CF-Connecting-IP")
	handler := server(t)
	const alb = "10.10.1.27:41234"
	const edge = "162.158.1.1"

	cases := []struct {
		name    string
		peer    string
		headers map[string][]string
		want    string
	}{
		{"through Cloudflare and the load balancer", alb,
			map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}, "X-Forwarded-For": {"198.51.100.7, " + edge}}, "198.51.100.7"},
		{"an IPv6 client", alb, map[string][]string{"CF-Connecting-IP": {"2001:db8::7"}}, "2001:db8::7"},
		{"a client spoofing X-Forwarded-For through Cloudflare", alb,
			map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}, "X-Forwarded-For": {"10.10.10.5, 198.51.100.7, " + edge}}, "198.51.100.7"},
		{"the header from a peer that is no proxy", "203.0.113.9:5000",
			map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}}, "203.0.113.9"},
		{"a header that is no address", alb,
			map[string][]string{"CF-Connecting-IP": {"198.51.100.7, 1.2.3.4"}, "X-Forwarded-For": {edge}}, edge},
		{"the header twice", alb,
			map[string][]string{"CF-Connecting-IP": {"198.51.100.7", "192.0.2.1"}, "X-Forwarded-For": {edge}}, edge},
		{"no header", alb, map[string][]string{"X-Forwarded-For": {edge}}, edge},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, clientIP(handler, tc.peer, tc.headers), tc.name)
	}
}

func TestTheHeaderIsIgnoredUnlessConfigured(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.10.1.0/24")
	t.Setenv("ESSERA_CLIENT_IP_HEADER", "")

	got := clientIP(server(t), "10.10.1.27:41234", map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}})

	assert.Equal(t, "10.10.1.27", got)
}

func TestAnInvalidTrustedProxyFailsStartUp(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "not-a-network")
	t.Setenv("ESSERA_CLIENT_IP_HEADER", "CF-Connecting-IP")

	_, err := clientip.Wrap(http.NotFoundHandler())

	assert.Error(t, err)
}
