package bridge

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/model"
)

// Latency is avg and percentiles in milliseconds; nil without requests.
type Latency struct {
	Avg *int64 `json:"avg"`
	P50 *int64 `json:"p50"`
	P95 *int64 `json:"p95"`
	P99 *int64 `json:"p99"`
}

type ModelTelemetry struct {
	Key                 string   `json:"key"`
	Requests            int64    `json:"requests"`
	Succeeded           int64    `json:"succeeded"`
	AvailabilityPercent *float64 `json:"availability_percent"`
	P95LatencyMs        *int64   `json:"p95_latency_ms"`
}

type ProviderTelemetry struct {
	Key                 string           `json:"key"`
	Requests            int64            `json:"requests"`
	Succeeded           int64            `json:"succeeded"`
	AvailabilityPercent *float64         `json:"availability_percent"`
	Latency             Latency          `json:"latency_ms"`
	Retries             int64            `json:"retries"`
	Timeouts            int64            `json:"timeouts"`
	Models              []ModelTelemetry `json:"models"`
}

type Window struct {
	Requests   int64               `json:"requests"`
	Succeeded  int64               `json:"succeeded"`
	Failed     int64               `json:"failed"`
	Latency    Latency             `json:"latency_ms"`
	HTTPStatus map[string]int64    `json:"http_status"`
	Denials    map[string]int64    `json:"denials"`
	Providers  []ProviderTelemetry `json:"providers"`
}

type RecentError struct {
	At       time.Time `json:"at"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Status   int       `json:"status"`
	Message  string    `json:"message"`
}

// Telemetry is {env}/gateway/telemetry.json.
type Telemetry struct {
	Schema       int               `json:"schema"`
	ObservedAt   time.Time         `json:"observed_at"`
	Windows      map[string]Window `json:"windows"`
	RecentErrors []RecentError     `json:"recent_errors"`
}

// aggregate adds up request rows: requests, successes, retries, timeouts, latency.
type aggregate struct {
	requests, succeeded, retries, timeouts, latencySum int64
	histogram                                          []int64
}

func (a *aggregate) add(row records.RequestStat) {
	a.requests += row.Requests
	a.retries += row.Retries
	a.latencySum += row.LatencySum
	if row.Outcome == "succeeded" {
		a.succeeded += row.Requests
	}
	if row.Outcome == "timeout" {
		a.timeouts += row.Requests
	}
	var histogram []int64
	_ = common.UnmarshalJsonStr(row.Histogram, &histogram)
	if len(a.histogram) < len(histogram) {
		a.histogram = append(a.histogram, make([]int64, len(histogram)-len(a.histogram))...)
	}
	for i, count := range histogram {
		a.histogram[i] += count
	}
}

func (a *aggregate) availability() *float64 {
	if a.requests == 0 {
		return nil
	}
	percent := float64(a.succeeded) * 100 / float64(a.requests)
	return &percent
}

func (a *aggregate) percentile(p float64) *int64 {
	if a.requests == 0 {
		return nil
	}
	target := int64(float64(a.requests)*p + 0.999999)
	var cumulative int64
	for i, count := range a.histogram {
		cumulative += count
		if cumulative >= target {
			edge := stats.LatencyEdges[min(i, len(stats.LatencyEdges)-1)]
			return &edge
		}
	}
	edge := stats.LatencyEdges[len(stats.LatencyEdges)-1]
	return &edge
}

func (a *aggregate) latency() Latency {
	if a.requests == 0 {
		return Latency{}
	}
	avg := a.latencySum / a.requests
	return Latency{Avg: &avg, P50: a.percentile(0.50), P95: a.percentile(0.95), P99: a.percentile(0.99)}
}

func window(rows []records.RequestStat) Window {
	result := Window{HTTPStatus: map[string]int64{}, Denials: map[string]int64{}}
	var total aggregate
	providers := map[string]*aggregate{}
	models := map[string]map[string]*aggregate{}
	for _, row := range rows {
		result.HTTPStatus[strconv.Itoa(row.HTTPStatus)] += row.Requests
		if reason, denied := strings.CutPrefix(row.Outcome, "denied:"); denied {
			result.Denials[reason] += row.Requests
			continue
		}
		total.add(row)
		if providers[row.Provider] == nil {
			providers[row.Provider] = &aggregate{}
			models[row.Provider] = map[string]*aggregate{}
		}
		providers[row.Provider].add(row)
		if models[row.Provider][row.Model] == nil {
			models[row.Provider][row.Model] = &aggregate{}
		}
		models[row.Provider][row.Model].add(row)
	}
	result.Requests, result.Succeeded = total.requests, total.succeeded
	result.Failed = total.requests - total.succeeded
	result.Latency = total.latency()
	for key, provider := range providers {
		entry := ProviderTelemetry{Key: key, Requests: provider.requests, Succeeded: provider.succeeded,
			AvailabilityPercent: provider.availability(), Latency: provider.latency(), Retries: provider.retries, Timeouts: provider.timeouts}
		for modelKey, m := range models[key] {
			entry.Models = append(entry.Models, ModelTelemetry{Key: modelKey, Requests: m.requests, Succeeded: m.succeeded,
				AvailabilityPercent: m.availability(), P95LatencyMs: m.percentile(0.95)})
		}
		sort.Slice(entry.Models, func(i, j int) bool { return entry.Models[i].Key < entry.Models[j].Key })
		result.Providers = append(result.Providers, entry)
	}
	sort.Slice(result.Providers, func(i, j int) bool { return result.Providers[i].Key < result.Providers[j].Key })
	return result
}

// CurrentTelemetry aggregates what every node recorded.
func CurrentTelemetry(now time.Time) (Telemetry, error) {
	var rows []records.RequestStat
	if err := model.DB.Where("bucket >= ?", now.Add(-7*24*time.Hour).Unix()).Find(&rows).Error; err != nil {
		return Telemetry{}, err
	}
	telemetry := Telemetry{Schema: 1, ObservedAt: now.UTC(), Windows: map[string]Window{}}
	for name, span := range map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour} {
		since := now.Add(-span).Unix()
		var inWindow []records.RequestStat
		for _, row := range rows {
			if row.Bucket >= since {
				inWindow = append(inWindow, row)
			}
		}
		telemetry.Windows[name] = window(inWindow)
	}
	var errors []records.ProviderError
	if err := model.DB.Where("at >= ?", now.Add(-24*time.Hour)).Order("at desc").Limit(50).Find(&errors).Error; err != nil {
		return Telemetry{}, err
	}
	for _, e := range errors {
		telemetry.RecentErrors = append(telemetry.RecentErrors, RecentError{At: e.At, Provider: e.Provider, Model: e.Model, Status: e.HTTPStatus, Message: e.Message})
	}
	return telemetry, nil
}

// PublishTelemetry writes telemetry.json.
func PublishTelemetry(ctx context.Context, telemetry Telemetry) error {
	bucket := configsync.Bucket()
	if bucket == "" {
		return nil
	}
	body, err := common.Marshal(telemetry)
	if err != nil {
		return err
	}
	return awsx.PutJSON(ctx, bucket, objectKey("telemetry.json"), body)
}
