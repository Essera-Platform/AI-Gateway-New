package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryAggregatesWhatEveryNodeRecorded(t *testing.T) {
	recordstest.UseDatabase(t)
	now := time.Now()
	for range 9 {
		stats.Request(now, "openai", "gpt-4o", "succeeded", 200, 800*time.Millisecond, 0)
	}
	stats.Request(now, "openai", "gpt-4o", "timeout", 504, 30*time.Second, 2)
	stats.Request(now.Add(-3*time.Hour), "openai", "gpt-4o", "succeeded", 200, 100*time.Millisecond, 0)
	stats.Denial(now, "country", 403)
	stats.ProviderError(now, "openai", "gpt-4o", 504, "upstream timeout")
	require.NoError(t, stats.Flush())
	stats.Request(now, "openai", "gpt-4o", "succeeded", 200, 800*time.Millisecond, 0)
	require.NoError(t, stats.Flush(), "a second flush adds to the same bucket")

	telemetry, err := CurrentTelemetry(now)

	require.NoError(t, err)
	hour := telemetry.Windows["1h"]
	assert.Equal(t, int64(11), hour.Requests)
	assert.Equal(t, int64(10), hour.Succeeded)
	assert.Equal(t, int64(1), hour.Failed)
	assert.Equal(t, map[string]int64{"200": 10, "504": 1, "403": 1}, hour.HTTPStatus, "denials count in the status distribution")
	assert.Equal(t, map[string]int64{"country": 1}, hour.Denials)
	require.Len(t, hour.Providers, 1)
	provider := hour.Providers[0]
	assert.Equal(t, "openai", provider.Key)
	assert.InDelta(t, 90.9, *provider.AvailabilityPercent, 0.1)
	assert.Equal(t, int64(2), provider.Retries)
	assert.Equal(t, int64(1), provider.Timeouts)
	assert.Equal(t, int64(1000), *provider.Latency.P50, "800 ms falls in the 1000 ms bucket")
	assert.Equal(t, int64(32000), *provider.Latency.P99)
	assert.Equal(t, int64(12), telemetry.Windows["24h"].Requests, "the older request is in the day")
	require.Len(t, telemetry.RecentErrors, 1)
	assert.Equal(t, "upstream timeout", telemetry.RecentErrors[0].Message)
}

func TestStatus(t *testing.T) {
	view := snapshottest.View()
	ok := []configsync.ProviderState{{Key: "openai", Status: configsync.ProviderOK}, {Key: "old", Status: configsync.ProviderDisabled}}
	assert.Equal(t, "unavailable", status(nil, ok, nil))
	assert.Equal(t, "operational", status(view, ok, nil))
	assert.Equal(t, "degraded", status(view, append(ok, configsync.ProviderState{Key: "b", Status: configsync.ProviderAutoDisabled}), nil))
	assert.Equal(t, "degraded", status(view, ok, []configsync.ApplyError{{Version: 8}}), "a newer version failed to apply")
	assert.Equal(t, "operational", status(view, ok, []configsync.ApplyError{{Version: 6}}), "an older failure was superseded")
	view.Snapshot.Gateway.MaintenanceStatus = snapshot.MaintenanceActive
	assert.Equal(t, "maintenance", status(view, ok, nil))
}

func TestStateNamesTheActiveVersionAndNodes(t *testing.T) {
	recordstest.UseDatabase(t, &model.SystemInstance{})
	now := time.Now()
	require.NoError(t, model.DB.Create(&model.SystemInstance{NodeName: "node-a", LastSeenAt: now.Unix()}).Error)
	require.NoError(t, model.DB.Create(&model.SystemInstance{NodeName: "gone", LastSeenAt: now.Add(-time.Hour).Unix()}).Error)
	snapshot.Activate(snapshottest.View())
	t.Cleanup(func() { snapshot.Activate(nil) })

	state := CurrentState(now, 3)

	assert.Equal(t, "operational", state.Status)
	require.NotNil(t, state.ActiveConfigurationVersion)
	assert.Equal(t, int64(7), *state.ActiveConfigurationVersion)
	require.Len(t, state.Nodes, 1)
	assert.Equal(t, "node-a", state.Nodes[0].Name)
	assert.Equal(t, int64(3), state.UsageBacklog)
	assert.Equal(t, 1, state.Schema)
}

func TestTrafficAnswersTheAskedWindow(t *testing.T) {
	recordstest.UseDatabase(t)
	now := time.Now()
	stats.Request(now, "openai", "gpt-4o", "succeeded", 200, 800*time.Millisecond, 1)
	stats.Request(now.Add(-3*time.Hour), "openai", "gpt-4o", "failed", 502, 100*time.Millisecond, 0)
	require.NoError(t, stats.Flush())

	ask := func(query string) (int, map[string]any) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/traffic"+query, nil)
		traffic(c)
		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		return recorder.Code, body
	}

	code, body := ask("?window=1h")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "1h", body["window"])
	hour := body["traffic"].(map[string]any)
	assert.EqualValues(t, 1, hour["requests"])
	assert.Equal(t, map[string]any{"200": float64(1)}, hour["http_status"])
	provider := hour["providers"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 1, provider["retries"])

	code, body = ask("")
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 2, body["traffic"].(map[string]any)["requests"], "a day by default")

	code, _ = ask("?window=30d")
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestTheStateCarriesTheTasksLimitsOnECS(t *testing.T) {
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Limits": {"CPU": 0.5, "Memory": 1024}}`))
	}))
	t.Cleanup(metadata.Close)
	t.Setenv("ECS_CONTAINER_METADATA_URI_V4", metadata.URL)
	limitsOnce, limits = sync.Once{}, taskLimits{}
	t.Cleanup(func() { limitsOnce, limits = sync.Once{}, taskLimits{} })
	recordstest.UseDatabase(t, &model.SystemInstance{})

	state := CurrentState(time.Now(), 0)

	require.NotNil(t, state.MemoryLimitMiB)
	assert.Equal(t, 1024.0, *state.MemoryLimitMiB)
	assert.Equal(t, 0.5, *state.CPULimitVCPU)
}
