package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry exports HTTP telemetry directly over OTLP/HTTP. It never reads
// request bodies, raw URLs, query strings, cookies, or authentication headers.
type Telemetry struct {
	traces   *sdktrace.TracerProvider
	metrics  *sdkmetric.MeterProvider
	logs     *sdklog.LoggerProvider
	tracer   trace.Tracer
	logger   otellog.Logger
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

// Start is opt-in: without OTEL_EXPORTER_OTLP_ENDPOINT, or when
// OTEL_SDK_DISABLED=true, no exporters or background workers are created.
// Exporters read the standard OTEL_EXPORTER_OTLP_* variables themselves,
// including headers and signal-specific endpoint overrides.
func Start(ctx context.Context, version string) (result *Telemetry, err error) {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return nil, nil
	}
	for _, signal := range []string{"", "TRACES_", "METRICS_", "LOGS_"} {
		protocol := os.Getenv("OTEL_EXPORTER_OTLP_" + signal + "PROTOCOL")
		if protocol != "" && protocol != "http/protobuf" {
			return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_%sPROTOCOL must be http/protobuf", signal)
		}
		endpoint := os.Getenv("OTEL_EXPORTER_OTLP_" + signal + "ENDPOINT")
		if endpoint == "" {
			continue
		}
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_%sENDPOINT must be an HTTP(S) URL without credentials, query or fragment", signal)
		}
	}

	t := &Telemetry{}
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = t.Shutdown(cleanupCtx)
		}
	}()
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", "new-api-backend"), attribute.String("service.version", version)),
		resource.WithFromEnv(),
	)
	if err != nil {
		// Resource values may contain secrets supplied through the environment.
		return nil, errors.New("invalid OpenTelemetry resource configuration")
	}
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, errors.New("failed to initialize OTLP trace exporter")
	}
	t.traces = sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traceExporter))
	t.tracer = t.traces.Tracer("github.com/QuantumNous/new-api/http")

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, errors.New("failed to initialize OTLP metric exporter")
	}
	t.metrics = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)))
	meter := t.metrics.Meter("github.com/QuantumNous/new-api/http")
	t.duration, err = meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600))
	if err != nil {
		return nil, err
	}
	t.active, err = meter.Int64UpDownCounter("http.server.active_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	heap, err := meter.Int64ObservableGauge("go.memory.used", metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	goroutines, err := meter.Int64ObservableGauge("go.goroutine.count", metric.WithUnit("{goroutine}"))
	if err != nil {
		return nil, err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		observer.ObserveInt64(heap, int64(stats.HeapAlloc))
		observer.ObserveInt64(goroutines, int64(runtime.NumGoroutine()))
		return nil
	}, heap, goroutines)
	if err != nil {
		return nil, err
	}
	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, errors.New("failed to initialize OTLP log exporter")
	}
	t.logs = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)))
	t.logger = t.logs.Logger("github.com/QuantumNous/new-api/http")
	otel.SetTracerProvider(t.traces)
	otel.SetMeterProvider(t.metrics)
	return t, nil
}

// Shutdown flushes all three signals; one export failure must not skip the rest.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	var errs []error
	if t.traces != nil {
		errs = append(errs, t.traces.Shutdown(ctx))
	}
	if t.metrics != nil {
		errs = append(errs, t.metrics.Shutdown(ctx))
	}
	if t.logs != nil {
		errs = append(errs, t.logs.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

// Middleware uses Gin's route templates to bound cardinality and exclude IDs
// and credentials. It leaves the original writer intact, including SSE flushes.
// Register it before recovery so recovered panics are recorded as HTTP 500.
func (t *Telemetry) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch:
		default:
			method = "_OTHER"
		}
		attrs := []attribute.KeyValue{attribute.String("http.request.method", method), attribute.String("http.route", route)}
		ctx := propagation.TraceContext{}.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := t.tracer.Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))
		c.Request = c.Request.WithContext(ctx)
		started := time.Now()
		t.active.Add(ctx, 1, metric.WithAttributes(attrs...))
		defer func() {
			status := c.Writer.Status()
			elapsed := time.Since(started)
			t.active.Add(ctx, -1, metric.WithAttributes(attrs...))
			attrs = append(attrs, attribute.Int("http.response.status_code", status))
			t.duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attrs...))
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			var record otellog.Record
			record.SetTimestamp(time.Now())
			record.SetBody(otellog.StringValue("HTTP request completed"))
			record.SetSeverity(otellog.SeverityInfo)
			if status >= http.StatusInternalServerError {
				record.SetSeverity(otellog.SeverityError)
			} else if status >= http.StatusBadRequest {
				record.SetSeverity(otellog.SeverityWarn)
			}
			record.AddAttributes(otellog.String("http.request.method", method), otellog.String("http.route", route),
				otellog.Int("http.response.status_code", status), otellog.Float64("http.server.request.duration", elapsed.Seconds()))
			t.logger.Emit(ctx, record)
			span.End()
		}()
		c.Next()
	}
}
