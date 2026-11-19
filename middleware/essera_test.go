// Essera's own test of its TokenAuth hook (CHANGES-ESSERA.md).
package middleware

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// esseraPeople applies the sample configuration, whose people become users, and returns the
// user id of each person by Cognito sub.
func esseraPeople(t *testing.T) map[string]int {
	t.Helper()
	t.Setenv("ESSERA_MANAGED", "true")
	recordstest.UseDatabase(t, &model.User{}, &model.UserSession{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.SystemTask{})
	redis := common.RedisEnabled
	common.RedisEnabled = false
	snapshot.Activate(nil)
	t.Cleanup(func() {
		common.RedisEnabled = redis
		snapshot.Activate(nil)
	})
	document, err := common.Marshal(snapshottest.Sample())
	require.NoError(t, err)
	require.NoError(t, configsync.Apply(3, "digest", document))
	var users []model.User
	require.NoError(t, model.DB.Where("oidc_id <> ''").Find(&users).Error)
	bySub := map[string]int{}
	for _, user := range users {
		bySub[user.OidcId] = user.Id
	}
	return bySub
}

// publish activates the sample configuration with the key of a person in it, as the Console
// does once it records credential.created.
func publish(t *testing.T, version int64, key string, created *keys.Created, edit func(*snapshot.Credential)) {
	t.Helper()
	s := snapshottest.Sample()
	credential := snapshot.Credential{
		Kind: snapshot.CredentialAPIKey, KeyID: created.KeyID, KeySHA256: keys.Digest(key), CustomerID: snapshottest.CustomerID,
		PersonID: snapshottest.UserPersonID, ExpiresAt: &created.ExpiresAt,
	}
	if edit != nil {
		edit(&credential)
	}
	s.Accounts.Credentials = append(s.Accounts.Credentials, credential)
	snapshot.Activate(snapshot.NewView(version, s))
}

func relayWith(t *testing.T, key string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("CF-IPCountry", "US")
	c.Request.Header.Set("X-Essera-End-User", "someone-else")
	_, handled := esseraAuthenticate(c, key)
	require.True(t, handled, "every key on a relay route is the integration's")
	return recorder, c
}

func outbox(t *testing.T) []map[string]any {
	t.Helper()
	var rows []records.OutboxEvent
	require.NoError(t, model.DB.Order("id").Find(&rows).Error)
	events := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var event map[string]any
		require.NoError(t, common.UnmarshalJsonStr(row.Body, &event))
		events = append(events, event)
	}
	return events
}

// finish reports an admitted request as served by the openai channel and returns its event.
func finish(t *testing.T, c *gin.Context) map[string]any {
	t.Helper()
	tag := "essera/openai"
	var channel model.Channel
	require.NoError(t, model.DB.Where("tag = ?", tag).First(&channel).Error)
	c.Set(common.RequestIdKey, "req-"+t.Name())
	common.SetContextKey(c, constant.ContextKeyConsumeLog, &model.RecordConsumeLogParams{PromptTokens: 10, CompletionTokens: 5})
	usage.Finished(c, &relaycommon.RelayInfo{
		StartTime: time.Now(), OriginModelName: "gpt-4o", RelayMode: relayconstant.RelayModeChatCompletions,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id},
	}, nil)
	events := outbox(t)
	return events[len(events)-1]
}

func TestAPersonsKeyWorksOnceTheConsolePublishesItAndUntilItIsRevoked(t *testing.T) {
	users := esseraPeople(t)
	now := time.Now()

	created, err := keys.Create(users[snapshottest.UserSub], "Production", 0, now)
	require.NoError(t, err)

	events := outbox(t)
	require.Len(t, events, 1, "credential.created goes to the Console with the key's row, in one transaction")
	event := events[0]
	assert.Equal(t, "credential.created", event["type"])
	assert.Equal(t, created.KeyID, event["key_id"])
	assert.Equal(t, keys.Digest(created.Key), event["key_sha256"])
	assert.Equal(t, created.Key[:7], event["key_prefix"])
	assert.Equal(t, "Production", event["name"])
	assert.Equal(t, snapshottest.CustomerID, event["customer_id"])
	assert.Equal(t, snapshottest.EndUserID, event["end_user_id"])
	assert.WithinDuration(t, now.Add(90*24*time.Hour), created.ExpiresAt, time.Second, "90 days unless shorter is chosen")
	assert.NotContains(t, event, "key", "the key itself is never sent")
	var token model.Token
	require.NoError(t, model.DB.First(&token, created.TokenID).Error)
	assert.NotContains(t, created.Key, token.Key, "the token row cannot be used or read back as the key")
	assert.Equal(t, created.Key[3:7], token.Key[:4], "it masks like the key")
	assert.Equal(t, created.Key[len(created.Key)-4:], token.Key[len(token.Key)-4:])
	assert.Equal(t, "essera", token.Group)

	recorder, _ := relayWith(t, created.Key)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code, "not in the snapshot yet")

	publish(t, 4, created.Key, created, nil)
	recorder, c := relayWith(t, created.Key)
	require.False(t, c.IsAborted(), recorder.Body.String())
	value, _ := c.Get(usage.ContextKey)
	request := value.(*policy.Request)
	assert.Equal(t, created.KeyID, request.Caller.CredentialRef)
	assert.Equal(t, snapshottest.CustomerID, request.Caller.Customer.ID)
	assert.Equal(t, snapshottest.PartnerID, request.Caller.Partner.ID)
	assert.Equal(t, "user-1", request.EndUserID, "the person's end user, not the one the request names")
	used := finish(t, c)
	assert.Equal(t, created.KeyID, used["credential_ref"])
	assert.Equal(t, "user-1", used["end_user_id"])

	forged := created.Key[:len(created.Key)-1] + "x"
	if forged == created.Key {
		forged = created.Key[:len(created.Key)-1] + "y"
	}
	for name, key := range map[string]string{"forged secret": forged, "short": "sk-" + created.KeyID[2:], "a JWT-less new-api key": "sk-abc123"} {
		recorder, _ := relayWith(t, key)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, name)
		assert.Contains(t, recorder.Body.String(), "The API key is not valid.", name)
	}

	revoked, err := keys.Revoke(users[snapshottest.UserSub], []int{created.TokenID}, now)
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	recorder, _ = relayWith(t, created.Key)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code, "deleted on the Gateway: refused before the Console republishes")
	last := outbox(t)
	assert.Equal(t, "credential.revoked", last[len(last)-1]["type"])
	assert.Equal(t, created.KeyID, last[len(last)-1]["key_id"])
	assert.Equal(t, "Deleted on the Gateway", last[len(last)-1]["reason"])
}

func TestAPublishedKeyIsRefusedWhenExpiredOrMovedToAnotherTenant(t *testing.T) {
	users := esseraPeople(t)
	created, err := keys.Create(users[snapshottest.UserSub], "Production", 0, time.Now())
	require.NoError(t, err)
	ended := time.Now().Add(-time.Second)

	cases := map[string]func(*snapshot.Credential){
		"expired":                    func(c *snapshot.Credential) { c.ExpiresAt = &ended },
		"of another tenant":          func(c *snapshot.Credential) { c.CustomerID = snapshottest.DirectCustomerID },
		"of another tenant's person": func(c *snapshot.Credential) { c.PersonID = snapshottest.DirectPersonID },
		"of a suspended person":      func(c *snapshot.Credential) { c.PersonID = snapshottest.SuspendedPersonID },
		"with another key's digest":  func(c *snapshot.Credential) { c.KeySHA256 = keys.Digest(snapshottest.APIKey) },
	}
	for name, edit := range cases {
		publish(t, 4, created.Key, created, edit)
		recorder, c := relayWith(t, created.Key)
		assert.True(t, c.IsAborted(), name)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, name)
	}
}

// OWASP ASVS 5.0 V9.2.1/V9.2.3 and V10.3.2: a Cognito client-credentials access token, the
// service credential the Console issued before API keys, is refused on relay even when it is
// validly signed by the customer pool, carries the old scope and names a client that a snapshot
// from before the retirement still lists.
func TestAFormerClientCredentialsTokenIsRefused(t *testing.T) {
	esseraPeople(t)
	t.Setenv("ESSERA_COGNITO_USER_POOL_ID", snapshottest.UserPoolID)
	t.Setenv("ESSERA_CUSTOMER_SCOPES", "essera-gateway/invoke")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	previous := auth.KeySetFor
	auth.KeySetFor = func(string) oidc.KeySet { return &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}} }
	t.Cleanup(func() { auth.KeySetFor = previous })

	document, err := common.Marshal(snapshottest.Sample())
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, common.Unmarshal(document, &raw))
	accounts := raw["accounts"].(map[string]any)
	accounts["credentials"] = append(accounts["credentials"].([]any),
		map[string]any{"client_id": "1h10cqee0u3rtkj1lu6cdr3eua", "customer_id": snapshottest.CustomerID, "expires_at": nil, "limits": map[string]any{}},
		map[string]any{"kind": "cognito_client", "client_id": "client-2", "customer_id": snapshottest.CustomerID, "expires_at": nil, "limits": map[string]any{}},
	)
	document, err = common.Marshal(raw)
	require.NoError(t, err)
	var legacy snapshot.Snapshot
	require.NoError(t, common.Unmarshal(document, &legacy))
	snapshot.Activate(snapshot.NewView(4, &legacy))

	now := time.Now()
	for _, clientID := range []string{"1h10cqee0u3rtkj1lu6cdr3eua", "client-2"} {
		token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": "https://cognito-idp.us-east-1.amazonaws.com/" + snapshottest.UserPoolID, "token_use": "access",
			"client_id": clientID, "sub": clientID, "scope": "essera-gateway/invoke",
			"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
		}).SignedString(key)
		require.NoError(t, err)

		recorder, c := relayWith(t, token)
		assert.True(t, c.IsAborted(), clientID)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, clientID)
		assert.Contains(t, recorder.Body.String(), "essera_unauthenticated", clientID)
		_, admitted := c.Get(usage.ContextKey)
		assert.False(t, admitted, "nothing is served or counted as the customer")
	}
}

func TestOnlyPeopleCreateKeysAndOperatorKeysStayRefusedOnRelay(t *testing.T) {
	users := esseraPeople(t)
	root := model.User{Username: "root", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "root", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&root).Error)

	_, err := keys.Create(root.Id, "mine", 0, time.Now())
	assert.ErrorIs(t, err, keys.ErrNotAPerson)
	_, err = keys.Create(users[snapshottest.SuspendedSub], "mine", 0, time.Now())
	assert.ErrorIs(t, err, keys.ErrNotAPerson, "a suspended person creates nothing")
	_, err = keys.Create(users[snapshottest.AdminSub], "past", time.Now().Add(-time.Hour).Unix(), time.Now())
	assert.ErrorIs(t, err, keys.ErrExpiryEnded)
	shorter := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	created, err := keys.Create(users[snapshottest.AdminSub], "short", shorter.Unix(), time.Now())
	require.NoError(t, err)
	assert.True(t, created.ExpiresAt.Equal(shorter))
	_, err = keys.Revoke(root.Id, []int{created.TokenID}, time.Now())
	assert.ErrorIs(t, err, keys.ErrNotAPerson)

	operatorKey := "sk-" + strings.Repeat("a", 48)
	require.NoError(t, model.DB.Create(&model.Token{UserId: root.Id, Key: strings.Repeat("a", 48), Status: common.TokenStatusEnabled, ExpiredTime: -1}).Error)
	publish(t, 4, created.Key, created, nil)
	recorder, _ := relayWith(t, operatorKey)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func playground(t *testing.T, userID int) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	recorder := httptest.NewRecorder()
	var reached *gin.Context
	engine := gin.New()
	engine.POST("/pg/chat/completions", func(c *gin.Context) { c.Set("id", userID) }, EsseraPlayground(), func(c *gin.Context) { reached = c })
	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	request.Header.Set("CF-IPCountry", "US")
	engine.ServeHTTP(recorder, request)
	return recorder, reached
}

func TestThePlaygroundRunsAsTheSignedInPerson(t *testing.T) {
	users := esseraPeople(t)
	root := model.User{Username: "root", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "root", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&root).Error)

	recorder, c := playground(t, users[snapshottest.UserSub])
	require.NotNil(t, c, recorder.Body.String())
	value, _ := c.Get(usage.ContextKey)
	request := value.(*policy.Request)
	assert.Equal(t, snapshottest.UserPersonID, request.Caller.Person.ID)
	assert.Empty(t, request.Caller.CredentialRef, "no credential in the playground")
	assert.Equal(t, "user-1", request.EndUserID)
	assert.Contains(t, request.AllowedModels, "gpt-4o", "the customer's entitlements")
	assert.True(t, c.GetBool("token_model_limit_enabled"))
	event := finish(t, c)
	assert.NotContains(t, event, "credential_ref")
	assert.Equal(t, "user-1", event["end_user_id"])
	assert.Equal(t, snapshottest.CustomerID, event["customer_id"])

	recorder, c = playground(t, users[snapshottest.AdminSub])
	require.NotNil(t, c, recorder.Body.String())
	value, _ = c.Get(usage.ContextKey)
	assert.Empty(t, value.(*policy.Request).EndUserID, "an admin has no end user")

	for name, userID := range map[string]int{"operator": root.Id, "suspended person": users[snapshottest.SuspendedSub]} {
		recorder, c := playground(t, userID)
		assert.Nil(t, c, name)
		assert.Equal(t, http.StatusForbidden, recorder.Code, name)
	}
}

func TestManagedRelayRefusesKeysThatAreNotConsoleCredentials(t *testing.T) {
	t.Setenv("ESSERA_MANAGED", "true")
	for path, refused := range map[string]bool{"/v1/chat/completions": true, "/v1beta/models/x:generateContent": true, "/api/usage/token": false} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)

		_, handled := esseraAuthenticate(c, "sk-abc123")

		assert.Equal(t, refused, handled, path)
		assert.Equal(t, refused, c.IsAborted(), path)
	}

	t.Setenv("ESSERA_MANAGED", "false")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, handled := esseraAuthenticate(c, "sk-abc123")
	assert.False(t, handled, "an unmanaged gateway is upstream's")
}

func TestRefusalsAfterThePolicyAreCountedOnceAndOnlyForCustomers(t *testing.T) {
	var counted []string
	countDenial = func(_ time.Time, reason string, status int) {
		counted = append(counted, fmt.Sprintf("%s %d", reason, status))
	}
	t.Cleanup(func() { countDenial = stats.Denial })
	refuse := func(admitted bool, status int) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		if admitted {
			c.Set(usage.ContextKey, &policy.Request{})
		}
		abortWithOpenAiMessage(c, status, "refused")
		abortWithOpenAiMessage(c, status, "refused again")
	}

	refuse(true, http.StatusForbidden)
	refuse(true, http.StatusServiceUnavailable)
	refuse(true, http.StatusBadRequest)
	refuse(false, http.StatusForbidden)

	assert.Equal(t, []string{"model 403", "no_channel 503", "invalid_request 400"}, counted,
		"a request the policy refused is counted by the policy, and an aborted one only once")
}

func TestAPersonDeletesOnlyTheirOwnKeys(t *testing.T) {
	users := esseraPeople(t)
	_, err := keys.Create(users[snapshottest.UserSub], "  ", 0, time.Now())
	assert.ErrorIs(t, err, keys.ErrNameMissing)
	created, err := keys.Create(users[snapshottest.UserSub], "Production", 0, time.Now())
	require.NoError(t, err)
	publish(t, 4, created.Key, created, nil)

	deleted, err := keys.Revoke(users[snapshottest.AdminSub], []int{created.TokenID}, time.Now())

	require.NoError(t, err)
	assert.Zero(t, deleted, "not even the admin deletes another person's key on the Gateway")
	recorder, c := relayWith(t, created.Key)
	assert.False(t, c.IsAborted(), recorder.Body.String())
	assert.Len(t, outbox(t), 1, "and no credential.revoked is sent for it")
}
