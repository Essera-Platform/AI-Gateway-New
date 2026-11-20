// Package configsync applies the configuration versions the Console publishes: it reads the
// configuration queue, fetches and verifies each snapshot, keeps the last good one in the
// Gateway's database, projects the provider catalog into new-api channels, the people into
// new-api users and the state of their API keys into the keys' token rows.
package configsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/secrets"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// SyncTopic tells every node to load the newest applied version from the database.
const SyncTopic = "essera_config"

// ApplyError is one version that could not be applied.
type ApplyError struct {
	Version int64     `json:"version"`
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

var (
	stateMu        sync.RWMutex
	providerStates []ProviderState
	applyErrors    []ApplyError
)

// ProviderStates is what the last applied version made of each provider.
func ProviderStates() []ProviderState {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return slices.Clone(providerStates)
}

// ApplyErrors lists the last versions that failed, newest first.
func ApplyErrors() []ApplyError {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return slices.Clone(applyErrors)
}

func rememberFailure(version int64, err error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	applyErrors = append([]ApplyError{{Version: version, At: time.Now().UTC(), Message: err.Error()}}, applyErrors...)
	if len(applyErrors) > 10 {
		applyErrors = applyErrors[:10]
	}
}

// HandleEvent applies the version an event announces. A version at or below the active one
// is already superseded and is acknowledged without work.
func HandleEvent(ctx context.Context, event snapshot.Event) error {
	if event.Environment != settings.Environment() {
		return fmt.Errorf("event for environment %q, this Gateway serves %q", event.Environment, settings.Environment())
	}
	if active := snapshot.Current(); active != nil && event.Version <= active.Version {
		return nil
	}
	if err := trusted(event, snapshot.Current()); err != nil {
		rememberFailure(event.Version, err)
		return nil // an event that names another place or an implausible version is never applied
	}
	data, found, err := awsx.GetObject(ctx, event.Bucket, event.SnapshotKey)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("snapshot %s is missing", event.SnapshotKey)
	}
	if digest := sha256.Sum256(data); hex.EncodeToString(digest[:]) != event.SHA256 {
		failure := fmt.Errorf("snapshot %s does not match its checksum", event.SnapshotKey)
		rememberFailure(event.Version, failure)
		return failure
	}
	if err := Apply(event.Version, event.SHA256, data); err != nil {
		rememberFailure(event.Version, err)
		var unreadable *unreadableSnapshot
		if errors.As(err, &unreadable) {
			recordFailure(event.Version, event.SHA256, data, err)
			return nil // a document that cannot be read stays failed; retrying will not help
		}
		return err // anything else, such as the database, may pass on the queue's next delivery
	}
	return nil
}

// unreadableSnapshot is a document that will never apply, as opposed to a failure on the way.
type unreadableSnapshot struct{ cause error }

func (e *unreadableSnapshot) Error() string { return "the snapshot cannot be read: " + e.cause.Error() }
func (e *unreadableSnapshot) Unwrap() error { return e.cause }

// Apply validates a snapshot document, keeps it and makes it the version every request is
// served with, on this node now and on every node within seconds.
func Apply(version int64, digest string, data []byte) error {
	parsed, err := snapshot.Parse(data)
	if err != nil {
		return &unreadableSnapshot{cause: err}
	}
	var previous *snapshot.Snapshot
	if active := snapshot.Current(); active != nil {
		previous = active.Snapshot
	}
	merged := snapshot.Merge(previous, parsed)
	states := ProviderStates()
	if merged.Providers != nil {
		states, err = projectProviders(merged.Providers, fallbackPriorities(merged.Providers))
		if err != nil {
			return err
		}
	}
	if err := projectPeople(merged.Accounts); err != nil {
		return err
	}
	document, err := common.Marshal(merged)
	if err != nil {
		return err
	}
	row := records.ConfigVersion{Version: version, SHA256: digest, Document: string(document), Applied: true, CreatedAt: time.Now().UTC()}
	if err := model.DB.Save(&row).Error; err != nil {
		return err
	}
	secrets.Forget()
	if common.MemoryCacheEnabled {
		// Requests are served with this version from the moment it is active, so this node's
		// channel cache must already hold its providers; other nodes follow the sync topics.
		model.InitChannelCache()
	}
	activate(version, merged, states)
	// Relay already follows the version; a key's state on the API keys page catches up at the
	// next version if this fails, so it does not hold this one back.
	if err := keys.Project(snapshot.Current(), time.Now()); err != nil {
		common.SysError(common.LogText("essera: projecting the API keys of version %d: %s", version, err.Error()))
	}
	model.BumpSyncTopic(model.SyncTopicChannels)
	model.BumpSyncTopic(SyncTopic)
	return nil
}

func recordFailure(version int64, digest string, data []byte, cause error) {
	row := records.ConfigVersion{Version: version, SHA256: digest, Document: string(data), Error: cause.Error(), CreatedAt: time.Now().UTC()}
	if err := model.DB.Save(&row).Error; err != nil {
		common.SysError(common.LogText("essera: recording failed version %d: %s", version, err.Error()))
	}
}

func activate(version int64, merged *snapshot.Snapshot, states []ProviderState) {
	snapshot.Activate(snapshot.NewView(version, merged))
	stateMu.Lock()
	providerStates = states
	stateMu.Unlock()
}

// LoadLatest activates the newest applied version from the database: at start-up, and on every
// node when the sync topic says another node applied one. It is the last good configuration,
// served when the pipeline is down.
func LoadLatest(context.Context) error {
	var row records.ConfigVersion
	err := model.DB.Where("applied = ?", true).Order("version desc").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if active := snapshot.Current(); active != nil && active.Version >= row.Version {
		return nil
	}
	parsed, err := snapshot.Parse([]byte(row.Document))
	if err != nil {
		return err
	}
	secrets.Forget()
	activate(row.Version, parsed, statesOfChannels(parsed))
	return nil
}

// statesOfChannels rebuilds the provider states of a loaded version from the channels the
// master projected, so every node reports the same thing.
func statesOfChannels(s *snapshot.Snapshot) []ProviderState {
	if s.Providers == nil {
		return nil
	}
	channels, err := managedChannels()
	if err != nil {
		return nil
	}
	states := make([]ProviderState, 0, len(s.Providers.Providers))
	for i := range s.Providers.Providers {
		provider := &s.Providers.Providers[i]
		state := ProviderState{Key: provider.Key, Status: ProviderOK}
		_, mapped := AdapterType(provider.Key)
		channel := channels[provider.Key]
		switch {
		case !mapped:
			state.Status = ProviderUnmapped
		case provider.Status != snapshot.StatusActive:
			state.Status = ProviderDisabled
		case provider.KeyReference == nil:
			state.Status = ProviderNoKey
		case channel != nil && channel.Status == common.ChannelStatusAutoDisabled:
			state.Status = ProviderAutoDisabled
		}
		states = append(states, state)
	}
	return states
}

// fallbackPriorities gives the providers that other providers fall back to a lower priority,
// so new-api retries move from a provider to its fallbacks (ESSERA_FALLBACK_POLICIES maps a
// policy reference to the provider keys it falls back to).
func fallbackPriorities(catalog *snapshot.ProviderCatalog) map[string]int64 {
	policies := FallbackPolicies()
	priorities := map[string]int64{}
	for _, provider := range catalog.Providers {
		priorities[provider.Key] = 10
	}
	for _, provider := range catalog.Providers {
		if provider.FallbackPolicyRef == nil {
			continue
		}
		for _, fallback := range policies[*provider.FallbackPolicyRef] {
			priorities[fallback] = 0
		}
	}
	return priorities
}

// FallbackPolicies are the policies the Gateway owns, by the reference the Console names them.
func FallbackPolicies() map[string][]string {
	policies := map[string][]string{}
	if raw := common.GetEnvOrDefaultString("ESSERA_FALLBACK_POLICIES", ""); raw != "" {
		_ = common.UnmarshalJsonStr(raw, &policies)
	}
	return policies
}

// MaxVersionJump bounds how far ahead of the active version an event may be. The Console
// numbers versions one after another, with gaps only for failed publications; a version far
// ahead would make every real one after it look superseded.
const MaxVersionJump = 1000

// trusted refuses an event outside this environment's snapshot bucket and prefix, or one whose
// version jumps implausibly far, whoever could put it on the queue.
func trusted(event snapshot.Event, active *snapshot.View) error {
	environment := settings.Environment()
	if !strings.HasPrefix(event.Bucket, "essera-"+environment+"-config-snapshots") {
		return fmt.Errorf("version %d names bucket %q, not this environment's snapshots", event.Version, event.Bucket)
	}
	if !strings.HasPrefix(event.SnapshotKey, environment+"/config/") {
		return fmt.Errorf("version %d names key %q outside %s/config/", event.Version, event.SnapshotKey, environment)
	}
	if active != nil && event.Version > active.Version+MaxVersionJump {
		return fmt.Errorf("version %d is more than %d ahead of the active %d", event.Version, MaxVersionJump, active.Version)
	}
	return nil
}
