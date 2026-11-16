package usage_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/auth"
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
	credential := view.Credential(snapshottest.ClientID)
	customer := view.Customer(credential.CustomerID)
	caller := &auth.Caller{ClientID: snapshottest.ClientID, Credential: credential, Customer: customer, Partner: view.Partner(customer.PartnerID), View: view}
	request, denial := policy.Evaluate(caller, endUser, "US", time.Now())
	require.Nil(t, denial)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Essera-Correlation-Id", "corr-1")
	c.Set(usage.ContextKey, request)
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
	assert.Equal(t, snapshottest.ClientID, event.CredentialRef)
	assert.Equal(t, "openai", event.Provider, "the Console's provider key, not the channel")
	assert.Equal(t, "gpt-4o", event.Model)
	assert.Equal(t, "text", event.Modality)
	assert.Equal(t, int64(1_000_000), event.InputUnits)
	assert.Equal(t, int64(500_000), event.OutputUnits)
	assert.Equal(t, "succeeded", event.Status)
	assert.Equal(t, "chatcmpl-9", *event.ProviderRef)
	assert.Equal(t, "corr-1", *event.CorrelationID)
	assert.Equal(t, "cost-negotiated", *event.CostRateVersion)
	assert.Equal(t, "sell-partner", *event.SellingRateVersion)
	assert.Equal(t, int64(7), event.ConfigurationVersion)
	assert.GreaterOrEqual(t, event.LatencyMs, int64(1500))
	assert.Equal(t, http.StatusOK, event.HTTPStatus)

	var counted records.Counter
	require.NoError(t, model.DB.Where("scope = ? AND subject = ?", "customer", snapshottest.CustomerID).First(&counted).Error)
	assert.Equal(t, int64(1), counted.Requests)
	assert.Equal(t, int64(1_500_000), counted.Tokens)
	assert.Equal(t, "8.4", counted.Spend.String(), "2.40 input at the partner's markup plus 6.00 output at the customer's rate")
}

func TestAFailedRequestIsReportedButNotCounted(t *testing.T) {
	s := serve(t, "")
	failure := types.NewErrorWithStatusCode(assert.AnError, types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)

	usage.Finished(s.context, s.info, failure)

	events := queued(t)
	require.Len(t, events, 1)
	assert.Equal(t, "rate_limited", events[0].Status)
	assert.Equal(t, http.StatusTooManyRequests, events[0].HTTPStatus)
	assert.Nil(t, events[0].EndUserID)
	var count int64
	model.DB.Model(&records.Counter{}).Count(&count)
	assert.Zero(t, count)
}

func TestARequestThatReachedNoProviderIsNotReported(t *testing.T) {
	s := serve(t, "")
	s.info.ChannelMeta = nil

	usage.Finished(s.context, s.info, types.NewError(assert.AnError, types.ErrorCodeGetChannelFailed))

	assert.Empty(t, queued(t))
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
