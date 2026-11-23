package usage_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/metering"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type served struct {
	context *gin.Context
	info    *relaycommon.RelayInfo
	request *policy.Request
}

func serve(t *testing.T, endUser string) served {
	t.Helper()
	recordstest.UseDatabase(t, &model.Channel{})
	tag := "essera/openai"
	channel := model.Channel{Tag: &tag, Name: "OpenAI", Key: "ref", Models: "gpt-4o", Group: "essera"}
	require.NoError(t, model.DB.Create(&channel).Error)

	view := snapshottest.View()
	credential := view.APIKey(snapshottest.AdminKeyID)
	customer := view.Customer(credential.CustomerID)
	caller := &auth.Caller{CredentialRef: snapshottest.AdminKeyID, Credential: credential, Customer: customer, Partner: view.Partner(customer.PartnerID),
		Person: view.Person(credential.PersonID), View: view}
	request, denial := policy.Evaluate(caller, endUser, "US", time.Now())
	require.Nil(t, denial)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Essera-Correlation-Id", "corr-1")
	c.Set(usage.ContextKey, request)
	require.Nil(t, usage.Reserve(c), "the relay reserves the request before its first attempt")
	c.Set(common.RequestIdKey, "req-1")
	c.Set(common.UpstreamRequestIdKey, "chatcmpl-9")
	common.SetContextKey(c, constant.ContextKeyConsumeLog, &model.RecordConsumeLogParams{PromptTokens: 1_000_000, CompletionTokens: 500_000})
	info := &relaycommon.RelayInfo{
		StartTime:       time.Now().Add(-1500 * time.Millisecond),
		OriginModelName: "gpt-4o",
		RelayMode:       relayconstant.RelayModeChatCompletions,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channel.Id},
	}
	return served{c, info, request}
}

func queued(t *testing.T) []usage.Event {
	t.Helper()
	var rows []records.OutboxEvent
	require.NoError(t, model.DB.Order("id").Find(&rows).Error)
	events := make([]usage.Event, 0, len(rows))
	for _, row := range rows {
		var event usage.Event
		require.NoError(t, common.UnmarshalJsonStr(row.Body, &event))
		events = append(events, event)
	}
	return events
}

func TestAServedRequestIsReportedAndCounted(t *testing.T) {
	s := serve(t, "user-1")

	usage.Finished(s.context, s.info, nil)

	events := queued(t)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, "gw-req-1", event.EventID)
	assert.Equal(t, snapshottest.CustomerID, event.CustomerID)
	assert.Equal(t, snapshottest.PartnerID, *event.TenantID)
	assert.Equal(t, "user-1", *event.EndUserID)
	assert.Equal(t, snapshottest.AdminKeyID, event.CredentialRef)
	assert.Equal(t, "openai", event.Provider, "the Console's provider key, not the channel")
	assert.Equal(t, "gpt-4o", event.Model)
	assert.Equal(t, "text", event.Modality)
	assert.Equal(t, int64(1_000_000), event.InputUnits)
	assert.Equal(t, int64(500_000), event.OutputUnits)
	assert.Equal(t, "succeeded", event.Status)
	assert.Equal(t, "chatcmpl-9", *event.ProviderRef)
	assert.Equal(t, "corr-1", *event.CorrelationID)
	assert.Equal(t, int64(7), event.ConfigurationVersion)
	assert.GreaterOrEqual(t, event.LatencyMs, int64(1500))
	assert.Equal(t, http.StatusOK, event.HTTPStatus)

	requests, tokens := counted(t, "customer", snapshottest.CustomerID)
	assert.Equal(t, int64(1), requests, "reserved when the policy admitted it")
	assert.Equal(t, int64(1_500_000), tokens)
}

func TestTheUnitsTheProviderBilledAreReportedApart(t *testing.T) {
	s := serve(t, "")
	metering.Record(s.context, metering.Units{Input: 10, CacheRead: 1000, CacheWrite: 20, CacheWrite1h: 30, Output: 5, InputImages: 2})

	usage.Finished(s.context, s.info, nil)

	events := queued(t)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, [6]int64{10, 1000, 20, 30, 5, 2},
		[6]int64{event.InputUnits, event.CacheReadUnits, event.CacheWriteUnits, event.CacheWrite1hUnits, event.OutputUnits, event.InputImageUnits},
		"the provider's own counts, not the settled prompt tokens")
	_, tokens := counted(t, "customer", snapshottest.CustomerID)
	assert.Equal(t, int64(1065), tokens, "the token limit counts cached tokens too")
}

func TestSpeechTokensAreReportedApartFromText(t *testing.T) {
	s := serve(t, "")
	metering.Record(s.context, metering.Units{Input: 351, Output: 32, SpeechInput: 264, SpeechOutput: 33})

	usage.Finished(s.context, s.info, nil)

	events := queued(t)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, [4]int64{351, 32, 264, 33}, [4]int64{event.InputUnits, event.OutputUnits, event.SpeechInputUnits, event.SpeechOutputUnits})
	_, tokens := counted(t, "customer", snapshottest.CustomerID)
	assert.Equal(t, int64(680), tokens, "the token limit counts speech tokens too")
}

func TestTheGatewayPricesNothing(t *testing.T) {
	s := serve(t, "user-1") // the snapshot carries rates for this model

	usage.Finished(s.context, s.info, nil)

	var rows []records.OutboxEvent
	require.NoError(t, model.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	var body map[string]any
	require.NoError(t, common.UnmarshalJsonStr(rows[0].Body, &body))
	for _, claim := range []string{"cost_rate_version", "selling_rate_version", "rate_versions"} {
		assert.NotContains(t, body, claim, "the Console rates every request; the event only measures it")
	}
	exceeded, err := limits.Exceeded(s.request.Subjects, s.request.Month, "USD")
	require.NoError(t, err)
	assert.Empty(t, exceeded, "1.5M tokens served, yet no spend until the Console publishes one")
}

// counted is what the Gateway itself counted for a subject this month, before any totals.
func counted(t *testing.T, scope, subject string) (requests, tokens int64) {
	t.Helper()
	var rows []records.LocalUse
	require.NoError(t, model.DB.Where("scope = ? AND subject = ?", scope, subject).Find(&rows).Error)
	for _, row := range rows {
		requests += row.Requests
		tokens += row.Tokens
	}
	return requests, tokens
}

func TestAFailedRequestIsReportedAndCountedAsTheConsoleCountsIt(t *testing.T) {
	s := serve(t, "")
	failure := types.NewErrorWithStatusCode(assert.AnError, types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)

	usage.Finished(s.context, s.info, failure)

	events := queued(t)
	require.Len(t, events, 1)
	assert.Equal(t, "rate_limited", events[0].Status)
	assert.Equal(t, http.StatusTooManyRequests, events[0].HTTPStatus)
	assert.Nil(t, events[0].EndUserID)
	requests, tokens := counted(t, "customer", snapshottest.CustomerID)
	assert.Equal(t, int64(1), requests, "every request counts, as in the Console")
	assert.Equal(t, int64(1_500_000), tokens, "the tokens it reported count too")
}

func TestARequestThatReachedNoProviderIsNotReported(t *testing.T) {
	s := serve(t, "")
	s.info.ChannelMeta = nil

	usage.Finished(s.context, s.info, types.NewError(assert.AnError, types.ErrorCodeGetChannelFailed))

	assert.Empty(t, queued(t))
}

func TestARealtimeSessionThatNeverCalledTheProviderIsNotReportedAndGivesItsRequestBack(t *testing.T) {
	s := serve(t, "")
	s.info.RelayMode = relayconstant.RelayModeRealtime
	s.context.Set(usage.ProviderNotCalledKey, true)
	metering.Record(s.context, metering.Units{})

	usage.Finished(s.context, s.info, nil)

	assert.Empty(t, queued(t), "no succeeded request with nothing billed")
	requests, _ := counted(t, "customer", snapshottest.CustomerID)
	assert.Zero(t, requests)
}

func TestOnlyEsseraRequestsAreReported(t *testing.T) {
	s := serve(t, "")
	s.context.Set(usage.ContextKey, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	usage.Finished(c, s.info, nil)

	assert.Empty(t, queued(t))
}

func TestAReplayedEventIsQueuedOnce(t *testing.T) {
	s := serve(t, "")
	usage.Finished(s.context, s.info, nil)
	usage.Finished(s.context, s.info, nil)
	assert.Len(t, queued(t), 1)
}

func TestTheLastRequestOfAQuotaGoesToOneRequestAndAnUnservedOneGivesItBack(t *testing.T) {
	s := serve(t, "user-1") // the end user's quota is 2; this request holds one
	admitted := func() *gin.Context {
		request, denial := policy.Evaluate(s.request.Caller, "user-1", "US", time.Now())
		require.Nil(t, denial)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(usage.ContextKey, request)
		return c
	}
	second, third := admitted(), admitted() // both pass the policy while one unit is left

	require.Nil(t, usage.Reserve(second))
	refused := usage.Reserve(third)
	require.NotNil(t, refused, "only one of them gets the last unit")
	assert.Equal(t, http.StatusTooManyRequests, refused.StatusCode)

	usage.Finished(second, &relaycommon.RelayInfo{}, nil) // it reached no provider
	assert.Nil(t, usage.Reserve(third), "the unserved request gave its unit back")
}
