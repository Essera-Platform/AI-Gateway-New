package configsync_test

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) {
	t.Helper()
	recordstest.UseDatabase(t, &model.Channel{}, &model.Ability{}, &model.SystemTask{})
	snapshot.Activate(nil)
	t.Cleanup(func() { snapshot.Activate(nil) })
}

func apply(t *testing.T, version int64, s *snapshot.Snapshot) {
	t.Helper()
	document, err := common.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, configsync.Apply(version, "digest", document))
}

func channels(t *testing.T) map[string]*model.Channel {
	t.Helper()
	var all []*model.Channel
	require.NoError(t, model.DB.Find(&all).Error)
	byTag := map[string]*model.Channel{}
	for _, channel := range all {
		byTag[channel.GetTag()] = channel
	}
	return byTag
}

func TestApplyProjectsProvidersIntoChannels(t *testing.T) {
	setup(t)

	apply(t, 3, snapshottest.Sample())

	projected := channels(t)
	require.Len(t, projected, 3)
	openai := projected["essera/openai"]
	assert.Equal(t, constant.ChannelTypeOpenAI, openai.Type)
	assert.Equal(t, "https://api.openai.com", openai.GetBaseURL(), "new-api adds the version path itself")
	assert.Equal(t, "gpt-4o", openai.Models, "only active models")
	assert.Equal(t, "essera", openai.Group)
	assert.Equal(t, "secretsmanager:essera/test/openai", openai.Key, "a reference, never the key")
	assert.Equal(t, common.ChannelStatusEnabled, openai.Status)
	bedrock := projected["essera/bedrock"]
	assert.Equal(t, constant.ChannelTypeAws, bedrock.Type)
	assert.Equal(t, "secretsmanager:essera/test/bedrock|us-east-1", bedrock.Key, "Bedrock's key carries its region")
	assert.Equal(t, "", bedrock.GetBaseURL())
	acme := projected["essera/acme-llm"]
	assert.Equal(t, common.ChannelStatusManuallyDisabled, acme.Status, "no adapter for acme-llm and no key")

	states := map[string]string{}
	for _, state := range configsync.ProviderStates() {
		states[state.Key] = state.Status
	}
	assert.Equal(t, map[string]string{"openai": "ok", "bedrock": "ok", "acme-llm": "unmapped"}, states)
	require.NotNil(t, snapshot.Current())
	assert.Equal(t, int64(3), snapshot.Current().Version)
}

func TestReapplyingFollowsTheCatalog(t *testing.T) {
	setup(t)
	apply(t, 3, snapshottest.Sample())
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("tag = ?", "essera/bedrock").Update("status", common.ChannelStatusAutoDisabled).Error)

	next := snapshottest.Sample()
	next.Providers.Providers = next.Providers.Providers[:2] // acme-llm removed
	next.Providers.Providers[0].Status = snapshot.StatusDisabled
	apply(t, 4, next)

	projected := channels(t)
	assert.NotContains(t, projected, "essera/acme-llm", "a removed provider's channel is deleted")
	assert.Equal(t, common.ChannelStatusManuallyDisabled, projected["essera/openai"].Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, projected["essera/bedrock"].Status, "the Gateway's own health decision stands")
}

func TestAVersionWithoutCatalogsKeepsThePreviousOnes(t *testing.T) {
	setup(t)
	apply(t, 3, snapshottest.Sample())

	settingsOnly := snapshottest.Sample()
	settingsOnly.Providers, settingsOnly.Accounts, settingsOnly.Rates = nil, nil, nil
	settingsOnly.Gateway.MaintenanceStatus = snapshot.MaintenanceActive
	apply(t, 4, settingsOnly)

	view := snapshot.Current()
	assert.Equal(t, int64(4), view.Version)
	assert.Equal(t, snapshot.MaintenanceActive, view.Snapshot.Gateway.MaintenanceStatus)
	assert.NotNil(t, view.Credential(snapshottest.ClientID), "accounts carried over")
	assert.Len(t, channels(t), 3, "providers carried over")
}

func TestLoadLatestServesTheLastGoodVersionAfterARestart(t *testing.T) {
	setup(t)
	apply(t, 5, snapshottest.Sample())
	require.NoError(t, model.DB.Create(&records.ConfigVersion{Version: 6, Document: "not json", Error: "broken"}).Error)
	snapshot.Activate(nil) // the process restarts

	require.NoError(t, configsync.LoadLatest(context.Background()))

	require.NotNil(t, snapshot.Current())
	assert.Equal(t, int64(5), snapshot.Current().Version, "a failed version is never served")
}

func TestApplyRefusesAnUnreadableDocument(t *testing.T) {
	setup(t)
	assert.Error(t, configsync.Apply(1, "digest", []byte(`{"providers":{}}`)))
	assert.Nil(t, snapshot.Current())
}

func TestAdapterType(t *testing.T) {
	t.Setenv("ESSERA_PROVIDER_TYPES", `{"acme-llm": 1}`)
	cases := map[string]int{"openai": constant.ChannelTypeOpenAI, "openai-eu": constant.ChannelTypeOpenAI, "google": constant.ChannelTypeGemini, "acme-llm": constant.ChannelTypeOpenAI}
	for key, want := range cases {
		got, ok := configsync.AdapterType(key)
		assert.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}
	_, ok := configsync.AdapterType("unknown")
	assert.False(t, ok)
}
