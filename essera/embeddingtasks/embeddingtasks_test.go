package embeddingtasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/aws"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const marengo = "twelvelabs.marengo-embed-3-0-v1:0"

func caller(t *testing.T, endUser string) *policy.Request {
	t.Helper()
	view := snapshottest.View()
	credential := view.APIKey(snapshottest.AdminKeyID)
	customer := view.Customer(credential.CustomerID)
	request, denial := policy.Evaluate(&auth.Caller{CredentialRef: snapshottest.AdminKeyID, Credential: credential, Customer: customer,
		Partner: view.Partner(customer.PartnerID), Person: view.Person(credential.PersonID), View: view}, endUser, "US", time.Now())
	require.Nil(t, denial)
	return request
}

// submit runs the end of a task submission as the relay does once Bedrock accepted it.
func submit(t *testing.T, taskID, modality string) {
	t.Helper()
	tag := "essera/bedrock"
	channel := model.Channel{Tag: &tag, Name: "Bedrock", Key: "ref", Models: marengo, Group: "essera"}
	require.NoError(t, model.DB.Create(&channel).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, aws.EmbeddingTaskPath, nil)
	c.Set(usage.ContextKey, caller(t, "user-1"))
	require.Nil(t, usage.Reserve(c))
	c.Set(common.RequestIdKey, "req-"+taskID)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), OriginModelName: marengo, RelayMode: relayconstant.RelayModeEmbeddings,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}}

	answer, err := Started(c, info, aws.EmbeddingTask{ID: taskID, Model: marengo, Modality: modality, InvocationARN: "arn:" + taskID, Region: "us-east-1"})
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSubmitted, answer.(Task).Status)
	usage.Finished(c, info, nil)
}

func read(t *testing.T, taskID, endUser string) (int, Task) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, aws.EmbeddingTaskPath+"/"+taskID, nil)
	c.Params = gin.Params{{Key: "task_id", Value: taskID}}
	c.Set(usage.ContextKey, caller(t, endUser))
	Get(c)
	var task Task
	_ = common.Unmarshal(recorder.Body.Bytes(), &task)
	return recorder.Code, task
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

// bedrock answers polls with the state of each invocation, by its ARN.
func bedrock(t *testing.T, states map[string]aws.EmbeddingTaskState, items []aws.EmbeddingTaskItem) {
	t.Helper()
	originalPoll, originalOutput := pollTask, taskOutput
	t.Cleanup(func() { pollTask, taskOutput = originalPoll, originalOutput })
	pollTask = func(_ context.Context, task aws.EmbeddingTask) (aws.EmbeddingTaskState, error) {
		return states[task.InvocationARN], nil
	}
	taskOutput = func(context.Context, string, string) ([]aws.EmbeddingTaskItem, error) { return items, nil }
}

func TestAVideoTaskIsReportedOnceWhenItEndsWithTheSecondsBedrockProcessed(t *testing.T) {
	t.Setenv("ESSERA_MANAGED", "true")
	recordstest.UseDatabase(t, &model.Channel{})
	states := map[string]aws.EmbeddingTaskState{}
	items := []aws.EmbeddingTaskItem{
		{Object: "embedding", Index: 0, Embedding: []float64{0.5}, EmbeddingOption: "visual", EmbeddingScope: "asset", StartSec: 0, EndSec: 4.6},
		{Object: "embedding", Index: 1, Embedding: []float64{0.25}, EmbeddingOption: "visual", EmbeddingScope: "clip", StartSec: 0, EndSec: 4.600000381469727},
	}
	bedrock(t, states, items)

	submit(t, "embtask-v", "video")
	assert.Empty(t, queued(t), "nothing is reported until the task ends")

	status, task := read(t, "embtask-v", "user-1")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.TaskStatusInProgress, task.Status)
	status, _ = read(t, "embtask-v", "user-2")
	assert.Equal(t, http.StatusNotFound, status, "another end user's task")

	states["arn:embtask-v"] = aws.EmbeddingTaskState{Done: true, OutputURI: "s3://out/embedding-tasks/embtask-v/inv"}
	Poll(context.Background())
	Poll(context.Background())
	status, task = read(t, "embtask-v", "user-1")

	events := queued(t)
	require.Len(t, events, 1, "a task is reported once, however often it is polled or read")
	event := events[0]
	assert.Equal(t, "gw-req-embtask-v", event.EventID)
	assert.Equal(t, "embedding", event.Modality)
	assert.Equal(t, "bedrock", event.Provider)
	assert.Equal(t, marengo, event.Model)
	assert.Equal(t, "succeeded", event.Status)
	assert.Equal(t, int64(5), event.InputVideoSeconds, "4.6 seconds of video, rounded up")
	assert.Zero(t, event.InputAudioSeconds)
	assert.Zero(t, event.InputUnits)
	assert.Equal(t, snapshottest.CustomerID, event.CustomerID)
	assert.Equal(t, "user-1", *event.EndUserID)

	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.TaskStatusSuccess, task.Status)
	assert.Equal(t, items, task.Data)
	assert.Equal(t, map[string]int64{"input_video_seconds": 5}, task.Usage)
	assert.NotZero(t, task.FinishedAt)

	status, _ = read(t, "embtask-v", "")
	assert.Equal(t, http.StatusOK, status, "the customer's admin reads their users' tasks")
	status, _ = read(t, "embtask-unknown", "user-1")
	assert.Equal(t, http.StatusNotFound, status)
}

func TestAFailedAudioTaskIsReportedFailedWithNoUnits(t *testing.T) {
	t.Setenv("ESSERA_MANAGED", "true")
	recordstest.UseDatabase(t, &model.Channel{})
	bedrock(t, map[string]aws.EmbeddingTaskState{"arn:embtask-a": {Done: true, FailReason: "unsupported audio format"}}, nil)

	submit(t, "embtask-a", "audio")
	status, task := read(t, "embtask-a", "user-1")

	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.TaskStatusFailure, task.Status)
	assert.Equal(t, "unsupported audio format", task.FailReason)
	events := queued(t)
	require.Len(t, events, 1)
	assert.Equal(t, "failed", events[0].Status)
	assert.Equal(t, http.StatusBadGateway, events[0].HTTPStatus)
	assert.Zero(t, events[0].InputAudioSeconds)
}
