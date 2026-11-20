package consoleapi

import (
	"context"
	"net/http"
	"time"
)

// ModelMetric is one model's performance over the last 24 hours as the Console measured it,
// from every customer's requests and its own probes: platform-wide, no customer's data. A
// figure without data is nil.
type ModelMetric struct {
	Provider           string     `json:"provider"`
	Model              string     `json:"model"`
	Availability24h    *float64   `json:"availability_24h"`
	P50TTFTMs          *int64     `json:"p50_ttft_ms"`
	P95TTFTMs          *int64     `json:"p95_ttft_ms"`
	P50LatencyMs       *int64     `json:"p50_latency_ms"`
	OutputPerSecond    *float64   `json:"output_per_second"`
	TrafficRequests24h int64      `json:"traffic_requests_24h"`
	ProbeRequests24h   int64      `json:"probe_requests_24h"`
	CheckedAt          *time.Time `json:"checked_at"`
}

// GetModelMetrics lists the metrics of the models the person may use that have any.
func GetModelMetrics(ctx context.Context, personID string) ([]ModelMetric, error) {
	var metrics struct {
		Data []ModelMetric `json:"data"`
	}
	return metrics.Data, call(ctx, http.MethodGet, personID, "/model-metrics", nil, nil, &metrics)
}

// PlatformMetrics is the whole platform over the last 7 days, for the landing page.
type PlatformMetrics struct {
	Span         string     `json:"span"`
	Availability *float64   `json:"availability"`
	P50TTFTMs    *int64     `json:"p50_ttft_ms"`
	Requests     int64      `json:"requests"`
	CheckedAt    *time.Time `json:"checked_at"`
}

// GetPlatformMetrics reads the platform summary; it names no person.
func GetPlatformMetrics(ctx context.Context) (*PlatformMetrics, error) {
	var metrics PlatformMetrics
	return &metrics, send(ctx, http.MethodGet, "", "/model-metrics/platform", nil, nil, &metrics)
}
