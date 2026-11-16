package configsync

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/secrets"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
)

// TagPrefix marks the channels the Console's providers are projected into; the rest of the
// tag is the provider key.
const TagPrefix = "essera/"

// Group is the routing group of every projected channel and of the Essera service user.
const Group = "essera"

// Provider statuses reported in the bridge state.
const (
	ProviderOK           = "ok"
	ProviderDisabled     = "disabled"
	ProviderAutoDisabled = "auto_disabled"
	ProviderUnmapped     = "unmapped"
	ProviderNoKey        = "no_key"
)

// ProviderState is what the Gateway made of one provider of the snapshot.
type ProviderState struct {
	Key     string  `json:"key"`
	Status  string  `json:"status"`
	Message *string `json:"message"`
}

var defaultAdapterTypes = map[string]int{
	"openai":     constant.ChannelTypeOpenAI,
	"azure":      constant.ChannelTypeAzure,
	"anthropic":  constant.ChannelTypeAnthropic,
	"gemini":     constant.ChannelTypeGemini,
	"google":     constant.ChannelTypeGemini,
	"vertex":     constant.ChannelTypeVertexAi,
	"bedrock":    constant.ChannelTypeAws,
	"aws":        constant.ChannelTypeAws,
	"mistral":    constant.ChannelTypeMistral,
	"deepseek":   constant.ChannelTypeDeepSeek,
	"xai":        constant.ChannelTypeXai,
	"cohere":     constant.ChannelTypeCohere,
	"openrouter": constant.ChannelTypeOpenRouter,
}

// AdapterType maps a Console provider key to the new-api adapter that calls it: an explicit
// entry in ESSERA_PROVIDER_TYPES ({"acme-llm": 1}) first, then the key itself, then the
// key's first dash-separated word ("openai-eu" → openai). ok is false when nothing matches.
func AdapterType(providerKey string) (int, bool) {
	var configured map[string]int
	if raw := common.GetEnvOrDefaultString("ESSERA_PROVIDER_TYPES", ""); raw != "" {
		_ = common.UnmarshalJsonStr(raw, &configured)
	}
	if adapter, ok := configured[providerKey]; ok {
		return adapter, true
	}
	if adapter, ok := defaultAdapterTypes[providerKey]; ok {
		return adapter, true
	}
	head, _, _ := strings.Cut(providerKey, "-")
	adapter, ok := defaultAdapterTypes[head]
	return adapter, ok
}

// baseURL is the address new-api appends its API paths to: the Console stores the versioned
// endpoint ("https://api.openai.com/v1"); AWS adapters take the region instead.
func baseURL(adapter int, endpoint string) string {
	if adapter == constant.ChannelTypeAws || adapter == constant.ChannelTypeVertexAi {
		return ""
	}
	trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	for _, version := range []string{"/v1beta", "/v1"} {
		if cut, ok := strings.CutSuffix(trimmed, version); ok {
			return cut
		}
	}
	return trimmed
}

func channelKey(adapter int, provider *snapshot.Provider) string {
	if provider.KeyReference == nil || *provider.KeyReference == "" {
		return ""
	}
	suffix := ""
	if adapter == constant.ChannelTypeAws {
		suffix = provider.Region
	}
	return secrets.Reference(*provider.KeyReference, suffix)
}

func activeModels(provider *snapshot.Provider) []string {
	models := make([]string, 0, len(provider.Models))
	for _, providerModel := range provider.Models {
		if providerModel.Status == snapshot.StatusActive {
			models = append(models, providerModel.Key)
		}
	}
	return models
}

// projectProviders makes the projected channels match the catalog: one channel per provider,
// tagged with its key, serving its active models in the Essera group. A channel the Gateway
// disabled after provider errors stays disabled until its health check re-enables it.
func projectProviders(catalog *snapshot.ProviderCatalog, fallbackPriority map[string]int64) ([]ProviderState, error) {
	existing, err := managedChannels()
	if err != nil {
		return nil, err
	}
	states := make([]ProviderState, 0, len(catalog.Providers))
	seen := map[string]bool{}
	for i := range catalog.Providers {
		provider := &catalog.Providers[i]
		seen[provider.Key] = true
		state, err := projectProvider(provider, existing[provider.Key], fallbackPriority[provider.Key])
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", provider.Key, err)
		}
		states = append(states, state)
	}
	for key, channel := range existing {
		if !seen[key] {
			if err := channel.Delete(); err != nil {
				return nil, fmt.Errorf("removing provider %s: %w", key, err)
			}
		}
	}
	return states, nil
}

func managedChannels() (map[string]*model.Channel, error) {
	var channels []*model.Channel
	if err := model.DB.Where("tag LIKE ?", TagPrefix+"%").Find(&channels).Error; err != nil {
		return nil, err
	}
	byKey := make(map[string]*model.Channel, len(channels))
	for _, channel := range channels {
		byKey[strings.TrimPrefix(channel.GetTag(), TagPrefix)] = channel
	}
	return byKey, nil
}

func projectProvider(provider *snapshot.Provider, channel *model.Channel, priority int64) (ProviderState, error) {
	state := ProviderState{Key: provider.Key, Status: ProviderOK}
	adapter, mapped := AdapterType(provider.Key)
	key := channelKey(adapter, provider)
	models := activeModels(provider)
	wanted := common.ChannelStatusEnabled
	switch {
	case !mapped:
		state.Status, state.Message = ProviderUnmapped, common.GetPointer("No adapter is configured for this provider key.")
		wanted = common.ChannelStatusManuallyDisabled
	case provider.Status != snapshot.StatusActive || len(models) == 0:
		state.Status = ProviderDisabled
		wanted = common.ChannelStatusManuallyDisabled
	case key == "":
		state.Status, state.Message = ProviderNoKey, common.GetPointer("No API key is configured for this provider.")
		wanted = common.ChannelStatusManuallyDisabled
	}
	if !mapped {
		adapter = constant.ChannelTypeOpenAI
	}
	if channel != nil && channel.Status == common.ChannelStatusAutoDisabled && wanted == common.ChannelStatusEnabled {
		wanted = common.ChannelStatusAutoDisabled
		state.Status = ProviderAutoDisabled
	}
	return state, saveChannel(channel, provider, adapter, key, models, wanted, priority)
}

// endpointOverrides replaces provider endpoints in development, where a provider is a local
// stand-in the Console's https-only endpoint field cannot name: ESSERA_PROVIDER_BASE_URLS is
// {"openai": "http://127.0.0.1:9100"}.
func endpointOverrides() map[string]string {
	overrides := map[string]string{}
	if raw := common.GetEnvOrDefaultString("ESSERA_PROVIDER_BASE_URLS", ""); raw != "" {
		_ = common.UnmarshalJsonStr(raw, &overrides)
	}
	return overrides
}

func saveChannel(channel *model.Channel, provider *snapshot.Provider, adapter int, key string, models []string, status int, priority int64) error {
	tag := TagPrefix + provider.Key
	base := baseURL(adapter, provider.Endpoint)
	if override, ok := endpointOverrides()[provider.Key]; ok {
		base = override
	}
	if key == "" {
		key = "unset"
	}
	if channel == nil {
		channel = &model.Channel{CreatedTime: common.GetTimestamp(), Key: key}
	}
	channel.Type = adapter
	channel.Name = provider.Name
	channel.Status = status
	channel.BaseURL = &base
	channel.Models = strings.Join(models, ",")
	channel.Group = Group
	channel.Tag = &tag
	channel.Priority = &priority
	if adapter == constant.ChannelTypeVertexAi {
		channel.Other = provider.Region
	}
	if channel.Id == 0 {
		return channel.Insert()
	}
	if err := channel.Update(); err != nil {
		return err
	}
	if channel.Key == key {
		return nil
	}
	_, err := model.UpdateChannelLocked(channel.Id, func(locked *model.Channel) error {
		locked.Key = key
		return nil
	})
	return err
}
