// Essera fork: Bedrock asynchronous video against fixtures of the Bedrock runtime API.
package aws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	asyncTestArn       = "arn:aws:bedrock:us-west-2:123456789012:async-invoke/inv0123"
	asyncTestAccessKey = "AKIDTASKROLE"
	asyncTestSecret    = "task-role-secret-never-in-a-url"
	asyncTestSession   = "task-role-session-token"
	asyncTestAPIKey    = "bedrock-api-key-not-for-async"
)

// bedrockFixture answers StartAsyncInvoke and GetAsyncInvoke as Bedrock does, in turn with
// the GetAsyncInvoke bodies given.
type bedrockFixture struct {
	mu        sync.Mutex
	started   map[string]any
	headers   []http.Header
	getBodies []string
	getStatus int
}

func (f *bedrockFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headers = append(f.headers, r.Header.Clone())
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/async-invoke":
		body, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(body, &f.started)
		_, _ = io.WriteString(w, `{"invocationArn":"`+asyncTestArn+`"}`)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/async-invoke/"):
		arn, _ := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/async-invoke/"))
		if arn != asyncTestArn || f.getStatus == http.StatusNotFound {
			w.Header().Set("X-Amzn-ErrorType", "ResourceNotFoundException")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"The invocation was not found"}`)
			return
		}
		body := f.getBodies[0]
		if len(f.getBodies) > 1 {
			f.getBodies = f.getBodies[1:]
		}
		_, _ = io.WriteString(w, body)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func useBedrockFixture(t *testing.T, fixture *bedrockFixture) {
	t.Helper()
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	previousEndpoint, previousCredentials := asyncBaseEndpoint, asyncTaskRoleCredentials
	asyncBaseEndpoint = server.URL
	asyncTaskRoleCredentials = func(context.Context) (aws.CredentialsProvider, error) {
		return credentials.NewStaticCredentialsProvider(asyncTestAccessKey, asyncTestSecret, asyncTestSession), nil
	}
	t.Cleanup(func() { asyncBaseEndpoint, asyncTaskRoleCredentials = previousEndpoint, previousCredentials })
	t.Setenv(AsyncOutputBucketEnv, "essera-dev-bedrock-output")
}

func submitVideo(t *testing.T, adaptor *AsyncVideoAdaptor, body string) (*relaycommon.RelayInfo, string) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ApiKey: asyncTestAPIKey + "|us-west-2", UpstreamModelName: "luma.ray-v2:0"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_abc123"},
	}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	resp, err := adaptor.DoRequest(c, info, requestBody)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	submitted, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	return info, submitted.UpstreamTaskID
}

func pollVideo(t *testing.T, task *model.Task) *relaycommon.TaskInfo {
	t.Helper()
	adaptor := &AsyncVideoAdaptor{}
	resp, err := adaptor.FetchTask("", "secretsmanager:bedrock|us-west-2", task, "")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	result, err := adaptor.ParseTaskResult(task, resp, body)
	require.NoError(t, err)
	task.Data = body
	return result
}

func TestBedrockVideoIsStartedInTheOutputBucketAndPolledUntilDone(t *testing.T) {
	fixture := &bedrockFixture{getBodies: []string{
		`{"invocationArn":"` + asyncTestArn + `","modelArn":"arn:aws:bedrock:us-west-2::foundation-model/luma.ray-v2:0","status":"InProgress","submitTime":"2026-11-19T10:00:00Z"}`,
		`{"invocationArn":"` + asyncTestArn + `","modelArn":"arn:aws:bedrock:us-west-2::foundation-model/luma.ray-v2:0","status":"Completed","submitTime":"2026-11-19T10:00:00Z","endTime":"2026-11-19T10:02:00Z","outputDataConfig":{"s3OutputDataConfig":{"s3Uri":"s3://essera-dev-bedrock-output/task_abc123/inv0123"}}}`,
	}}
	useBedrockFixture(t, fixture)
	adaptor := &AsyncVideoAdaptor{}

	info, invocationArn := submitVideo(t, adaptor, `{"model":"luma.ray-v2:0","prompt":"a red fox in snow","seconds":"9","size":"720x1280","metadata":{"loop":true}}`)

	assert.Equal(t, asyncTestArn, invocationArn)
	assert.Equal(t, map[string]float64{"seconds": 9}, adaptor.EstimateBilling(nil, info))
	assert.Equal(t, map[string]any{
		"clientRequestToken": "abc123",
		"modelId":            "luma.ray-v2:0",
		"modelInput":         map[string]any{"prompt": "a red fox in snow", "aspect_ratio": "9:16", "duration": "9s", "resolution": "720p", "loop": true},
		"outputDataConfig":   map[string]any{"s3OutputDataConfig": map[string]any{"s3Uri": "s3://essera-dev-bedrock-output/task_abc123"}},
	}, fixture.started)

	task := &model.Task{TaskID: "task_abc123", Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{UpstreamTaskID: invocationArn}}
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), model.TaskStatus(pollVideo(t, task).Status))
	done := pollVideo(t, task)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), model.TaskStatus(done.Status))
	assert.Empty(t, done.Url, "the stored task keeps no signed URL; each read signs one")

	for _, header := range fixture.headers {
		authorization := header.Get("Authorization")
		assert.Contains(t, authorization, "Credential="+asyncTestAccessKey+"/", "Bedrock is called with the task role")
		assert.Contains(t, authorization, "/us-west-2/bedrock/aws4_request")
		assert.Equal(t, asyncTestSession, header.Get("X-Amz-Security-Token"))
		for _, values := range header {
			assert.NotContains(t, strings.Join(values, " "), asyncTestAPIKey, "the API key cannot write to S3 and is never sent")
		}
	}

	task.Status = model.TaskStatusSuccess
	signed, err := adaptor.PresignResult(context.Background(), task)
	require.NoError(t, err)
	location, err := url.Parse(signed)
	require.NoError(t, err)
	assert.Equal(t, "https", location.Scheme)
	assert.Equal(t, "essera-dev-bedrock-output.s3.us-west-2.amazonaws.com", location.Host)
	assert.Equal(t, "/task_abc123/inv0123/output.mp4", location.Path)
	query := location.Query()
	assert.Equal(t, "900", query.Get("X-Amz-Expires"), "the URL lives 15 minutes")
	assert.Equal(t, "GetObject", query.Get("x-id"))
	for name := range query {
		assert.Contains(t, []string{"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature", "X-Amz-Security-Token", "x-id"}, name)
	}
	assert.NotContains(t, signed, asyncTestSecret)
	assert.NotContains(t, signed, asyncTestAPIKey)
	assert.True(t, strings.HasPrefix(query.Get("X-Amz-Credential"), asyncTestAccessKey+"/"), "the credential names the key id and scope only")
}

func TestBedrockVideoFailureAndMissingInvocation(t *testing.T) {
	fixture := &bedrockFixture{getBodies: []string{
		`{"invocationArn":"` + asyncTestArn + `","status":"Failed","failureMessage":"The prompt was blocked by content filters."}`,
	}}
	useBedrockFixture(t, fixture)

	task := &model.Task{TaskID: "task_abc123", PrivateData: model.TaskPrivateData{UpstreamTaskID: asyncTestArn}}
	failed := pollVideo(t, task)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), model.TaskStatus(failed.Status))
	assert.Equal(t, "The prompt was blocked by content filters.", failed.Reason)

	fixture.getStatus = http.StatusNotFound
	resp, err := (&AsyncVideoAdaptor{}).FetchTask("", "ak|sk|us-west-2", task, "")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "the poller fails a task Bedrock does not know")
	assert.Contains(t, fixture.headers[len(fixture.headers)-1].Get("Authorization"), "Credential=ak/", "a channel holding an access key signs with it")
}

func TestBedrockVideoRequestsAreBoundedToWhatTheModelGenerates(t *testing.T) {
	image := "data:image/png;base64,iVBORw0KGgo="
	cases := []struct {
		name    string
		model   string
		request relaycommon.TaskSubmitReq
		input   map[string]any
		seconds int
		err     string
	}{
		{name: "reel shot", model: "amazon.nova-reel-v1:1", request: relaycommon.TaskSubmitReq{Prompt: "p", Images: []string{image}, Metadata: map[string]any{"seed": float64(7)}},
			input: map[string]any{"taskType": "TEXT_VIDEO", "textToVideoParams": map[string]any{"text": "p", "images": []any{map[string]any{"format": "png", "source": map[string]any{"bytes": "iVBORw0KGgo="}}}}, "videoGenerationConfig": map[string]any{"durationSeconds": 6, "fps": 24, "dimension": "1280x720", "seed": int64(7)}}, seconds: 6},
		{name: "reel multi-shot", model: "amazon.nova-reel-v1:1", request: relaycommon.TaskSubmitReq{Prompt: "p", Duration: 18},
			input: map[string]any{"taskType": "MULTI_SHOT_AUTOMATED", "multiShotAutomatedParams": map[string]any{"text": "p"}, "videoGenerationConfig": map[string]any{"durationSeconds": 18, "fps": 24, "dimension": "1280x720"}}, seconds: 18},
		{name: "reel v1:0 has one shot", model: "amazon.nova-reel-v1:0", request: relaycommon.TaskSubmitReq{Prompt: "p", Duration: 12}, err: "seconds must be 6"},
		{name: "reel beyond two minutes", model: "amazon.nova-reel-v1:1", request: relaycommon.TaskSubmitReq{Prompt: "p", Duration: 126}, err: "seconds must be 6"},
		{name: "reel image by URL", model: "amazon.nova-reel-v1:1", request: relaycommon.TaskSubmitReq{Prompt: "p", Images: []string{"https://example.com/a.png"}}, err: "base64 data URL"},
		{name: "ray default", model: "luma.ray-v2:0", request: relaycommon.TaskSubmitReq{Prompt: "p"},
			input: map[string]any{"prompt": "p", "aspect_ratio": "16:9", "duration": "5s", "resolution": "720p"}, seconds: 5},
		{name: "ray other length", model: "luma.ray-v2:0", request: relaycommon.TaskSubmitReq{Prompt: "p", Seconds: "6"}, err: "seconds must be 5 or 9"},
		{name: "ray from an image", model: "luma.ray-v2:0", request: relaycommon.TaskSubmitReq{Prompt: "p", Images: []string{image}}, err: "from text only"},
		{name: "ray unknown ratio", model: "luma.ray-v2:0", request: relaycommon.TaskSubmitReq{Prompt: "p", Metadata: map[string]any{"aspect_ratio": "2:1"}}, err: "aspect_ratio"},
	}
	for _, tc := range cases {
		var input map[string]any
		var seconds int
		var err error
		if strings.HasPrefix(tc.model, "luma") {
			input, seconds, err = lumaRayInput(tc.request)
		} else {
			input, seconds, err = novaReelInput(tc.model, tc.request)
		}
		if tc.err != "" {
			assert.ErrorContains(t, err, tc.err, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.input, input, tc.name)
		assert.Equal(t, tc.seconds, seconds, tc.name)
	}
}
