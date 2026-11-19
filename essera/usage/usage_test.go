package usage_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/limits"
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

	var counted, spent records.Counter
	require.NoError(t, model.DB.Where("scope = ? AND subject = ? AND currency = ?", "customer", snapshottest.CustomerID, limits.Counts).First(&counted).Error)
	assert.Equal(t, int64(1), counted.Requests, "reserved when the policy admitted it")
	assert.Equal(t, int64(1_500_000), counted.Tokens)
	require.NoError(t, model.DB.Where("scope = ? AND subject = ? AND currency = ?", "customer", snapshottest.CustomerID, "USD").First(&spent).Error)
	assert.Equal(t, "8.4", spent.Spend.String(), "2.40 input at the partner's markup plus 6.00 output at the customer's rate")
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
	var counted records.Counter
	require.NoError(t, model.DB.Where("scope = ? AND subject = ? AND currency = ?", "customer", snapshottest.CustomerID, limits.Counts).First(&counted).Error)
	assert.Equal(t, int64(1), counted.Requests, "every request counts, as in the Console")
	assert.Equal(t, int64(1_500_000), counted.Tokens, "the tokens it reported count too")
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

const videoInvocation = "arn:aws:bedrock:us-west-2:123456789012:async-invoke/inv0123"

// submitVideoTask submits a Bedrock video task as the relay does once the provider accepted it.
func submitVideoTask(t *testing.T, taskID string) *model.Task {
	t.Helper()
	s := serve(t, "user-1")
	tag := "essera/bedrock"
	channel := model.Channel{Tag: &tag, Name: "Bedrock", Key: "ref", Models: "luma.ray-v2:0", Group: "essera"}
	require.NoError(t, model.DB.Create(&channel).Error)
	s.context.Set(common.RequestIdKey, "req-"+taskID)
	s.info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channel.Id}
	s.info.OriginModelName = "luma.ray-v2:0"
	s.info.RelayMode = relayconstant.RelayModeVideoSubmit
	s.info.PriceData.AddOtherRatio("seconds", 9)
	task := &model.Task{TaskID: taskID, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{UpstreamTaskID: videoInvocation}}
	usage.TaskSubmission(s.context, s.info, task, nil)
	return task
}

func TestAVideoTaskIsReportedOnceWhenItEndsWithTheCallerOfItsSubmission(t *testing.T) {
	task := submitVideoTask(t, "task_ok")
	assert.Empty(t, queued(t), "nothing is generated until the video is done")
	owner, found, err := usage.TaskCustomer("task_ok")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, snapshottest.CustomerID, owner)

	task.Status, task.FinishTime = model.TaskStatusSuccess, time.Now().Unix()
	usage.TaskFinished(task)
	usage.TaskFinished(task)

	events := queued(t)
	require.Len(t, events, 1, "a task is reported once, however often it is ended")
	event := events[0]
	assert.Equal(t, "gw-req-task_ok", event.EventID)
	assert.Equal(t, "video", event.Modality)
	assert.Equal(t, int64(9), event.GeneratedUnits, "video seconds")
	assert.Zero(t, event.InputUnits)
	assert.Zero(t, event.OutputUnits)
	assert.Equal(t, "succeeded", event.Status)
	assert.Equal(t, snapshottest.ClientID, event.CredentialRef, "the credential that submitted it")
	assert.Equal(t, snapshottest.CustomerID, event.CustomerID)
	assert.Equal(t, snapshottest.PartnerID, *event.TenantID)
	assert.Equal(t, "user-1", *event.EndUserID)
	assert.Equal(t, "bedrock", event.Provider)
	assert.Equal(t, "luma.ray-v2:0", event.Model)
	assert.Equal(t, videoInvocation, *event.ProviderRef)
	assert.Equal(t, int64(7), event.ConfigurationVersion)
	var counted records.Counter
	require.NoError(t, model.DB.Where("scope = ? AND subject = ? AND currency = ?", "customer", snapshottest.CustomerID, limits.Counts).First(&counted).Error)
	assert.Equal(t, int64(1), counted.Requests)
}

func TestAFailedVideoTaskIsReportedFailedWithNoUnits(t *testing.T) {
	task := submitVideoTask(t, "task_failed")

	task.Status, task.FailReason = model.TaskStatusFailure, "blocked by content filters"
	usage.TaskFinished(task)

	events := queued(t)
	require.Len(t, events, 1)
	assert.Equal(t, "failed", events[0].Status)
	assert.Zero(t, events[0].GeneratedUnits)
	assert.Equal(t, http.StatusBadGateway, events[0].HTTPStatus)
	assert.Equal(t, "task_failed", *events[0].ErrorCategory)
	assert.Equal(t, snapshottest.ClientID, events[0].CredentialRef)
}

func TestAVideoSubmissionThatFailsIsReportedOnlyIfItReachedTheProvider(t *testing.T) {
	s := serve(t, "")
	s.info.RelayMode = relayconstant.RelayModeVideoSubmit
	usage.TaskSubmission(s.context, s.info, nil, &taskdto.TaskError{Code: "fail_to_fetch_task", Message: "ValidationException", StatusCode: http.StatusBadRequest})
	events := queued(t)
	require.Len(t, events, 1, "the provider refused it")
	assert.Equal(t, "video", events[0].Modality)
	assert.Equal(t, "failed", events[0].Status)
	assert.Zero(t, events[0].GeneratedUnits)

	refused := serve(t, "")
	usage.TaskSubmission(refused.context, refused.info, nil, &taskdto.TaskError{Code: "invalid_request", Message: "seconds must be 5 or 9", StatusCode: http.StatusBadRequest, LocalError: true})
	assert.Empty(t, queued(t), "the gateway refused it before any provider")
	var counted records.Counter
	require.NoError(t, model.DB.Where("scope = ? AND subject = ? AND currency = ?", "customer", snapshottest.CustomerID, limits.Counts).First(&counted).Error)
	assert.Zero(t, counted.Requests, "its reservation is given back")
	_, found, err := usage.TaskCustomer("task_unknown")
	require.NoError(t, err)
	assert.False(t, found)
}
