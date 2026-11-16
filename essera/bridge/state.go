// Package bridge reports the Gateway to the Console through objects in the configuration
// bucket (docs/handoff/gateway-bridge.md in the Console repository): its state, its
// telemetry and the results of connection tests the Console asks for.
package bridge

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
)

// State is {env}/gateway/state.json.
type State struct {
	Schema                     int                        `json:"schema"`
	Environment                string                     `json:"environment"`
	ObservedAt                 time.Time                  `json:"observed_at"`
	Status                     string                     `json:"status"`
	ActiveConfigurationVersion *int64                     `json:"active_configuration_version"`
	Build                      string                     `json:"build"`
	StartedAt                  time.Time                  `json:"started_at"`
	UptimeSeconds              int64                      `json:"uptime_seconds"`
	CPUPercent                 float64                    `json:"cpu_percent"`
	MemoryUsedMiB              float64                    `json:"memory_used_mib"`
	RetryTimes                 int                        `json:"retry_times"`
	Nodes                      []Node                     `json:"nodes"`
	Providers                  []configsync.ProviderState `json:"providers"`
	ApplyErrors                []configsync.ApplyError    `json:"apply_errors"`
	FallbackPolicies           []string                   `json:"fallback_policies"`
	UsageBacklog               int64                      `json:"usage_backlog"`
}

// Node is one Gateway process seen recently.
type Node struct {
	Name           string    `json:"name"`
	AppliedVersion *int64    `json:"applied_version"`
	SeenAt         time.Time `json:"seen_at"`
}

func objectKey(name string) string {
	return fmt.Sprintf("%s/gateway/%s", settings.Environment(), name)
}

// CurrentState describes the Gateway now.
func CurrentState(now time.Time, usageBacklog int64) State {
	view := snapshot.Current()
	started := time.Unix(common.StartTime, 0).UTC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	state := State{
		Schema:           1,
		Environment:      settings.Environment(),
		ObservedAt:       now.UTC(),
		Build:            common.Version,
		StartedAt:        started,
		UptimeSeconds:    int64(now.Sub(started).Seconds()),
		CPUPercent:       common.GetSystemStatus().CPUUsage,
		MemoryUsedMiB:    float64(memory.Sys) / (1 << 20),
		RetryTimes:       common.RetryTimes,
		Nodes:            nodes(now, view),
		Providers:        configsync.ProviderStates(),
		ApplyErrors:      configsync.ApplyErrors(),
		FallbackPolicies: policyNames(),
		UsageBacklog:     usageBacklog,
	}
	if view != nil {
		state.ActiveConfigurationVersion = &view.Version
	}
	state.Status = status(view, state.Providers, state.ApplyErrors)
	return state
}

func status(view *snapshot.View, providers []configsync.ProviderState, failures []configsync.ApplyError) string {
	if view == nil {
		return "unavailable"
	}
	if view.Snapshot.Gateway.MaintenanceStatus == snapshot.MaintenanceActive {
		return "maintenance"
	}
	if len(failures) > 0 && failures[0].Version > view.Version {
		return "degraded"
	}
	for _, provider := range providers {
		if provider.Status != configsync.ProviderOK && provider.Status != configsync.ProviderDisabled {
			return "degraded"
		}
	}
	return "operational"
}

func nodes(now time.Time, view *snapshot.View) []Node {
	instances, err := model.ListSystemInstances()
	if err != nil {
		return nil
	}
	var seen []Node
	for _, instance := range instances {
		at := time.Unix(instance.LastSeenAt, 0).UTC()
		if now.Sub(at) > 2*time.Minute {
			continue
		}
		node := Node{Name: instance.NodeName, SeenAt: at}
		if view != nil {
			node.AppliedVersion = &view.Version // every node loads the version the master applied
		}
		seen = append(seen, node)
	}
	return seen
}

func policyNames() []string {
	names := []string{}
	for name := range configsync.FallbackPolicies() {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// PublishState writes state.json.
func PublishState(ctx context.Context, state State) error {
	bucket := configsync.Bucket()
	if bucket == "" {
		return nil
	}
	body, err := common.Marshal(state)
	if err != nil {
		return err
	}
	return awsx.PutJSON(ctx, bucket, objectKey("state.json"), body)
}
