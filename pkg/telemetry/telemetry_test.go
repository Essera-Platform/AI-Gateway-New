package telemetry

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestStartIsOptInAndRejectsInvalidConfigurationWithoutLeakingIt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endpoint string
		protocol string
		disabled string
		wantErr  bool
	}{
		{name: "no endpoint"},
		{name: "explicitly disabled", endpoint: "https://example.test/otlp", disabled: "true"},
		{name: "unsupported protocol", endpoint: "https://example.test/otlp", protocol: "grpc", wantErr: true},
		{name: "credentials in URL", endpoint: "https://user:private-value@example.test/otlp", wantErr: true},
		{name: "query in URL", endpoint: "https://example.test/otlp?token=private-value", wantErr: true},
		{name: "malformed URL", endpoint: ":private-value", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", tc.protocol)
			t.Setenv("OTEL_SDK_DISABLED", tc.disabled)
			telemetry, err := Start(context.Background(), "test")
			require.Nil(t, telemetry)
			if tc.wantErr {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-value")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHTTPExportsCorrelatedOTLPWithoutRequestSecretsAndPreservesStreaming(t *testing.T) {
	type receivedExport struct {
		path          string
		authorization string
		body          []byte
		err           error
	}
	var mu sync.Mutex
	var exports []receivedExport
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		mu.Lock()
		exports = append(exports, receivedExport{r.URL.Path, r.Header.Get("Authorization"), body, err})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	for _, signal := range []string{"", "TRACES_", "METRICS_", "LOGS_"} {
		for _, key := range []string{"ENDPOINT", "PROTOCOL", "HEADERS", "COMPRESSION"} {
			t.Setenv("OTEL_EXPORTER_OTLP_"+signal+key, "")
		}
	}
	t.Setenv("OTEL_SDK_DISABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL+"/otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic test-export-token")
	t.Setenv("OTEL_SERVICE_NAME", "gateway-test")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=test")
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_always_on")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	oldTracer, oldMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTracer)
		otel.SetMeterProvider(oldMeter)
	})
	telemetry, err := Start(context.Background(), "test-version")
	require.NoError(t, err)
	require.NotNil(t, telemetry)
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })

	engine := gin.New()
	engine.Use(telemetry.Middleware())
	engine.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	engine.GET("/items/:id", func(c *gin.Context) {
		_, _ = io.Copy(io.Discard, c.Request.Body)
		c.Status(http.StatusAccepted)
	})
	engine.GET("/panic", func(_ *gin.Context) { panic("private-panic-value") })
	engine.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: first\n\n")
		c.Writer.Flush()
		_, _ = c.Writer.WriteString("data: second\n\n")
	})
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/items/private-path-value?code=private-query-value", http.StatusAccepted},
		{"/unknown-private-path-value", http.StatusNotFound},
		{"/panic", http.StatusInternalServerError},
		{"/stream", http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodGet, tc.path, strings.NewReader("private-body-value"))
		request.Header.Set("Authorization", "Bearer private-header-value")
		request.Header.Set("Cookie", "session=private-cookie-value")
		request.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, tc.status, response.Code)
		if tc.path == "/stream" {
			assert.True(t, response.Flushed)
			assert.Equal(t, "data: first\n\ndata: second\n\n", response.Body.String())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, telemetry.Shutdown(ctx))

	mu.Lock()
	received := append([]receivedExport(nil), exports...)
	mu.Unlock()
	var traces collectortrace.ExportTraceServiceRequest
	var metrics collectormetrics.ExportMetricsServiceRequest
	var logs collectorlogs.ExportLogsServiceRequest
	for _, export := range received {
		require.NoError(t, export.err)
		assert.Equal(t, "Basic test-export-token", export.authorization)
		assert.NotContains(t, string(export.body), "private-")
		assert.NotContains(t, string(export.body), "test-export-token")
		switch export.path {
		case "/otlp/v1/traces":
			require.NoError(t, proto.Unmarshal(export.body, &traces))
		case "/otlp/v1/metrics":
			require.NoError(t, proto.Unmarshal(export.body, &metrics))
		case "/otlp/v1/logs":
			require.NoError(t, proto.Unmarshal(export.body, &logs))
		default:
			t.Errorf("unexpected OTLP path: %s", export.path)
		}
	}
	require.Len(t, traces.ResourceSpans, 1)
	assert.Contains(t, traces.ResourceSpans[0].Resource.String(), "gateway-test")
	assert.Contains(t, traces.ResourceSpans[0].Resource.String(), "test-version")
	spans := traces.ResourceSpans[0].ScopeSpans[0].Spans
	require.Len(t, spans, 4)
	for _, span := range spans {
		assert.Equal(t, "0123456789abcdef0123456789abcdef", hex.EncodeToString(span.TraceId))
		assert.Equal(t, "0123456789abcdef", hex.EncodeToString(span.ParentSpanId))
		if span.Name == "GET /panic" {
			assert.Equal(t, tracepb.Status_STATUS_CODE_ERROR, span.Status.Code)
		}
	}
	require.Len(t, logs.ResourceLogs, 1)
	records := logs.ResourceLogs[0].ScopeLogs[0].LogRecords
	require.Len(t, records, 4)
	for _, record := range records {
		assert.Equal(t, "0123456789abcdef0123456789abcdef", hex.EncodeToString(record.TraceId))
		assert.NotEmpty(t, record.SpanId)
		assert.Equal(t, "HTTP request completed", record.Body.GetStringValue())
	}
	require.Len(t, metrics.ResourceMetrics, 1)
	var completed uint64
	var active int64
	for _, metric := range metrics.ResourceMetrics[0].ScopeMetrics[0].Metrics {
		switch metric.Name {
		case "http.server.request.duration":
			for _, point := range metric.GetHistogram().DataPoints {
				completed += point.Count
			}
		case "http.server.active_requests":
			for _, point := range metric.GetSum().DataPoints {
				active += point.GetAsInt()
			}
		}
	}
	assert.Equal(t, uint64(4), completed)
	assert.Zero(t, active)
}
