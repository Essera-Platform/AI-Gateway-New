package configsync_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) {
	t.Helper()
	recordstest.UseDatabase(t, &model.Channel{}, &model.Ability{}, &model.SystemTask{}, &model.User{}, &model.UserSession{}, &model.Token{})
	redis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = redis })
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
	assert.NotNil(t, view.APIKey(snapshottest.KeyID), "accounts carried over")
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

func usersBySub(t *testing.T) map[string]model.User {
	t.Helper()
	var all []model.User
	require.NoError(t, model.DB.Where("oidc_id <> ''").Find(&all).Error)
	bySub := map[string]model.User{}
	for _, user := range all {
		bySub[user.OidcId] = user
	}
	return bySub
}

func TestPeopleAreProjectedIntoUsersAndLeaveWithTheirSessions(t *testing.T) {
	setup(t)
	operator := model.User{Username: "operator", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Email: "owner@direct.example", AffCode: "op", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&operator).Error)

	apply(t, 3, snapshottest.Sample())

	users := usersBySub(t)
	require.Len(t, users, 3, "the direct customer's owner shares the operator's email, so has no user")
	admin, endUser, suspended := users[snapshottest.AdminSub], users[snapshottest.UserSub], users[snapshottest.SuspendedSub]
	assert.Equal(t, "owner@acme.example", admin.Username)
	assert.Equal(t, common.RoleCommonUser, admin.Role, "an admin is no Gateway administrator")
	assert.Equal(t, "essera", admin.Group)
	assert.Equal(t, common.UserStatusEnabled, admin.Status)
	assert.Positive(t, admin.Quota)
	assert.Empty(t, admin.Password, "a person has no password to sign in with")
	assert.True(t, admin.GetSetting().AcceptUnsetRatioModel)
	assert.Equal(t, common.UserStatusDisabled, suspended.Status)
	var links []records.PersonUser
	require.NoError(t, model.DB.Order("person_id").Find(&links).Error)
	require.Len(t, links, 3)
	assert.Equal(t, snapshottest.AdminPersonID, links[0].PersonID)
	assert.Equal(t, admin.Id, links[0].UserID)
	assert.Equal(t, "admin", links[0].Role, "the person's role lives in the Essera table")
	assert.Equal(t, "user", links[1].Role)

	_, err := service.CreateLoginSession(endUser.Id, "oauth:oidc", "127.0.0.1", "test")
	require.NoError(t, err)
	apply(t, 4, snapshottest.Sample())
	again := usersBySub(t)
	assert.Equal(t, endUser.Id, again[snapshottest.UserSub].Id, "applying again changes nobody")
	assert.Equal(t, endUser.AuthVersion, again[snapshottest.UserSub].AuthVersion)
	active, err := model.CountActiveUserSessions(endUser.Id, time.Now().Unix())
	require.NoError(t, err)
	assert.EqualValues(t, 1, active)

	next := snapshottest.Sample()
	next.Accounts.People[1].Status = "suspended"                     // the end user
	next.Accounts.People = next.Accounts.People[:3]                  // the direct owner stays absent
	next.Accounts.People = slices.Delete(next.Accounts.People, 0, 1) // the admin is removed
	apply(t, 5, next)

	after := usersBySub(t)
	assert.Equal(t, common.UserStatusDisabled, after[snapshottest.UserSub].Status, "suspended")
	assert.Greater(t, after[snapshottest.UserSub].AuthVersion, endUser.AuthVersion, "its tokens and sessions are void")
	active, err = model.CountActiveUserSessions(endUser.Id, time.Now().Unix())
	require.NoError(t, err)
	assert.Zero(t, active, "suspension ends the session at once")
	assert.Equal(t, common.UserStatusDisabled, after[snapshottest.AdminSub].Status, "removed")
	assert.Greater(t, after[snapshottest.AdminSub].AuthVersion, admin.AuthVersion)
	var unchanged model.User
	require.NoError(t, model.DB.First(&unchanged, operator.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, unchanged.Status, "operators are never touched")
	assert.Equal(t, common.RoleRootUser, unchanged.Role)
	assert.EqualValues(t, 1, unchanged.AuthVersion)

	apply(t, 6, snapshottest.Sample())
	assert.Equal(t, common.UserStatusEnabled, usersBySub(t)[snapshottest.UserSub].Status, "reactivated")
}

func TestAVersionBeforeThePeopleLeavesUsersAlone(t *testing.T) {
	setup(t)
	apply(t, 3, snapshottest.Sample())

	withoutPeople := snapshottest.Sample()
	withoutPeople.Accounts.People = nil
	apply(t, 4, withoutPeople)

	assert.Equal(t, common.UserStatusEnabled, usersBySub(t)[snapshottest.AdminSub].Status)
}

// key adds a person's key the way essera/keys keeps it: an essera_keys row and its token row.
func key(t *testing.T, keyID string, created, expires time.Time, published, deleted *time.Time) int {
	t.Helper()
	token := model.Token{UserId: 1, Name: keyID, Key: keyID, Status: common.TokenStatusEnabled, CreatedTime: created.Unix(), ExpiredTime: expires.Unix(), UnlimitedQuota: true}
	require.NoError(t, model.DB.Create(&token).Error)
	require.NoError(t, model.DB.Create(&records.Key{KeyID: keyID, TokenID: token.Id, UserID: 1, PersonID: snapshottest.UserPersonID,
		CustomerID: snapshottest.CustomerID, ExpiresAt: expires, CreatedAt: created, PublishedAt: published, RevokedAt: deleted}).Error)
	return token.Id
}

func TestKeyStatesFollowTheConsole(t *testing.T) {
	setup(t)
	now := time.Now().UTC().Truncate(time.Second)
	later := now.Add(30 * 24 * time.Hour)
	earlier := now.Add(-time.Hour)
	listed := key(t, snapshottest.KeyID, earlier, later, nil, nil)
	revoked := key(t, "k_revoked", earlier, later, &earlier, nil)
	pending := key(t, "k_pending", now, later, nil, nil)
	neverPublished := key(t, "k_never_published", earlier, later, nil, nil)
	expired := key(t, "k_expired", now.Add(-48*time.Hour), earlier, &earlier, nil)
	deleted := key(t, "k_deleted", earlier, later, &earlier, &now)

	apply(t, 3, snapshottest.Sample())

	states := func() map[int]model.Token {
		var tokens []model.Token
		require.NoError(t, model.DB.Find(&tokens).Error)
		byID := map[int]model.Token{}
		for _, token := range tokens {
			byID[token.Id] = token
		}
		return byID
	}
	tokens := states()
	assert.Equal(t, common.TokenStatusEnabled, tokens[listed].Status, "published")
	assert.Equal(t, time.Date(2027, 2, 17, 0, 0, 0, 0, time.UTC).Unix(), tokens[listed].ExpiredTime, "the published expiry")
	assert.Equal(t, keys.TokenStatusRevoked, tokens[revoked].Status, "published before, gone now")
	assert.Equal(t, common.TokenStatusEnabled, tokens[pending].Status, "created a moment ago: the Console has not published it yet")
	assert.Equal(t, keys.TokenStatusRevoked, tokens[neverPublished].Status, "the Console never published it")
	assert.Equal(t, common.TokenStatusExpired, tokens[expired].Status)
	assert.Equal(t, common.TokenStatusEnabled, tokens[deleted].Status, "deleted on the Gateway: not the Console's to change")
	var row records.Key
	require.NoError(t, model.DB.First(&row, "key_id = ?", snapshottest.KeyID).Error)
	require.NotNil(t, row.PublishedAt)

	next := snapshottest.Sample()
	next.Accounts.Credentials = next.Accounts.Credentials[:2] // the Console revokes the end user's key
	apply(t, 4, next)
	assert.Equal(t, keys.TokenStatusRevoked, states()[listed].Status, "revoked at once, once published")

	withoutAccounts := snapshottest.Sample()
	withoutAccounts.Accounts = nil
	apply(t, 5, withoutAccounts)
	assert.Equal(t, keys.TokenStatusRevoked, states()[listed].Status, "a version without accounts keeps the previous ones")
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
