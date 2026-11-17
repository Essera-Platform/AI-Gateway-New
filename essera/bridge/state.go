// Package bridge is the Gateway's internal API for the Platform Backend (the Console's
// GatewayClient, apps/backend/app/modules/integrations/gateway.py in the Console repository):
// the Gateway's state and active configuration version, a provider's connection test and its
// telemetry.
package bridge

import (
	"runtime"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
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
	CPULimitVCPU               *float64                   `json:"cpu_limit_vcpu"`
	MemoryLimitMiB             *float64                   `json:"memory_limit_mib"`
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

// CurrentState describes the Gateway now.
func CurrentState(now time.Time, usageBacklog int64) State {
	view := snapshot.Current()
	started := time.Unix(common.StartTime, 0).UTC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	task := readTaskLimits()
	state := State{
		Schema:           1,
		Environment:      settings.Environment(),
		ObservedAt:       now.UTC(),
		Build:            common.Version,
		StartedAt:        started,
		UptimeSeconds:    int64(now.Sub(started).Seconds()),
		CPUPercent:       common.GetSystemStatus().CPUUsage,
		MemoryUsedMiB:    float64(memory.Sys) / (1 << 20),
		CPULimitVCPU:     task.CPU,
		MemoryLimitMiB:   task.Memory,
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
	if state.Providers == nil {
		state.Providers = []configsync.ProviderState{}
	}
	if state.ApplyErrors == nil {
		state.ApplyErrors = []configsync.ApplyError{}
	}
	if state.Nodes == nil {
		state.Nodes = []Node{}
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
