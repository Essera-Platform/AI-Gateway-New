package bridge

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/servicetrust"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/gin-gonic/gin"
)

// Prefix is where the internal API lives. The Gateway's public frontend does not proxy it; it
// is reached only inside the cluster.
const Prefix = "/internal/essera/v1"

// RegisterRoutes adds the internal API when the Gateway is managed by the Console.
func RegisterRoutes(router *gin.Engine) {
	if !settings.Managed() {
		return
	}
	group := router.Group(Prefix, trustBackend)
	group.GET("/state", state)
	group.POST("/providers/:key/test", testProvider)
	group.GET("/providers/:key/telemetry", providerTelemetry)
}

func trustBackend(c *gin.Context) {
	if _, err := servicetrust.Verify(c.Request.Context(), c.GetHeader(servicetrust.Header)); err != nil {
		common.SysLog(common.LogText("essera: internal API refused: %s", err.Error()))
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "The caller's identity is not trusted."})
		return
	}
	c.Next()
}

// state answers GatewayClient.state(): status, active configuration version, observed at;
// the rest is extra detail.
func state(c *gin.Context) {
	backlog, _ := usage.Backlog()
	c.JSON(http.StatusOK, CurrentState(time.Now(), backlog))
}

type connectionResult struct {
	Succeeded bool    `json:"succeeded"`
	LatencyMs *int64  `json:"latency_ms"`
	Message   *string `json:"message"`
}

// testProvider answers GatewayClient.test_provider(key).
func testProvider(c *gin.Context) {
	result := TestProvider(c.Request.Context(), c.Param("key"))
	reply := connectionResult{Succeeded: result.Succeeded}
	if result.Latency > 0 {
		latency := result.Latency.Milliseconds()
		reply.LatencyMs = &latency
	}
	if result.Message != "" {
		reply.Message = &result.Message
	}
	c.JSON(http.StatusOK, reply)
}

type providerError struct {
	At      time.Time `json:"at"`
	Model   *string   `json:"model"`
	Message string    `json:"message"`
}

type providerTelemetryReply struct {
	AvailabilityPercent *float64         `json:"availability_percent"`
	P50LatencyMs        *int64           `json:"p50_latency_ms"`
	P95LatencyMs        *int64           `json:"p95_latency_ms"`
	P99LatencyMs        *int64           `json:"p99_latency_ms"`
	AvgLatencyMs        *int64           `json:"avg_latency_ms"`
	Requests            int64            `json:"requests"`
	Retries             int64            `json:"retries"`
	Timeouts            int64            `json:"timeouts"`
	Models              []ModelTelemetry `json:"models"`
	RecentErrors        []providerError  `json:"recent_errors"`
}

// providerTelemetry answers GatewayClient.provider_telemetry(key): the last hour.
func providerTelemetry(c *gin.Context) {
	telemetry, err := CurrentTelemetry(time.Now())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "Telemetry is not available right now."})
		return
	}
	key := c.Param("key")
	reply := providerTelemetryReply{Models: []ModelTelemetry{}, RecentErrors: []providerError{}}
	for _, provider := range telemetry.Windows["1h"].Providers {
		if provider.Key != key {
			continue
		}
		reply.AvailabilityPercent = provider.AvailabilityPercent
		reply.P50LatencyMs, reply.P95LatencyMs = provider.Latency.P50, provider.Latency.P95
		reply.P99LatencyMs, reply.AvgLatencyMs = provider.Latency.P99, provider.Latency.Avg
		reply.Requests, reply.Retries, reply.Timeouts = provider.Requests, provider.Retries, provider.Timeouts
		reply.Models = provider.Models
	}
	for _, e := range telemetry.RecentErrors {
		if e.Provider != key {
			continue
		}
		entry := providerError{At: e.At, Message: e.Message}
		if e.Model != "" {
			entry.Model = &e.Model
		}
		reply.RecentErrors = append(reply.RecentErrors, entry)
	}
	c.JSON(http.StatusOK, reply)
}
