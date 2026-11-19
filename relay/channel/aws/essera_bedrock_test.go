// Essera fork: Bedrock Converse, embedding and error mapping tests,
// against recorded Bedrock Runtime bodies served by an httptest server.
package aws

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/MoeForge/moego/ojson"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/smithy-go"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

type capturedBedrockRequest struct {
	path   string
	header http.Header
	body   []byte
}

// bedrockReplyFixture is what the stub answers to one request.
type bedrockReplyFixture struct {
	status int
	header map[string]string
	body   []byte
}

// startBedrockStub serves the fixtures in order and records each request. The
// relay's HTTP client is the default client while no proxy is configured.
func startBedrockStub(t *testing.T, fixtures ...bedrockReplyFixture) (*httptest.Server, func() []capturedBedrockRequest) {
	t.Helper()
	var mu sync.Mutex
	var captured []capturedBedrockRequest
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		index := len(captured)
		captured = append(captured, capturedBedrockRequest{path: r.URL.EscapedPath(), header: r.Header.Clone(), body: body})
		mu.Unlock()
		fixture := fixtures[min(index, len(fixtures)-1)]
		for name, value := range fixture.header {
			w.Header().Set(name, value)
		}
		status := fixture.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(fixture.body)
	}))
	originalClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() {
		http.DefaultClient = originalClient
		server.Close()
	})
	return server, func() []capturedBedrockRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedBedrockRequest(nil), captured...)
	}
}

func jsonFixture(body string) bedrockReplyFixture {
	return bedrockReplyFixture{header: map[string]string{"Content-Type": "application/json"}, body: []byte(body)}
}

// converseStreamFixture encodes ConverseStream events; an event type that
// starts with "!" is an exception of that type.
func converseStreamFixture(t *testing.T, events ...[2]string) bedrockReplyFixture {
	t.Helper()
	var buffer bytes.Buffer
	encoder := eventstream.NewEncoder()
	for _, event := range events {
		headers := eventstream.Headers{{Name: ":content-type", Value: eventstream.StringValue("application/json")}}
		if name, isException := strings.CutPrefix(event[0], "!"); isException {
			headers = append(headers,
				eventstream.Header{Name: ":message-type", Value: eventstream.StringValue("exception")},
				eventstream.Header{Name: ":exception-type", Value: eventstream.StringValue(name)})
		} else {
			headers = append(headers,
				eventstream.Header{Name: ":message-type", Value: eventstream.StringValue("event")},
				eventstream.Header{Name: ":event-type", Value: eventstream.StringValue(event[0])})
		}
		require.NoError(t, encoder.Encode(&buffer, eventstream.Message{Headers: headers, Payload: []byte(event[1])}))
	}
	return bedrockReplyFixture{header: map[string]string{"Content-Type": "application/vnd.amazon.eventstream"}, body: buffer.Bytes()}
}

func newBedrockTestInfo(server *httptest.Server, mode int, format relaytypes.RelayFormat, model string, stream bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		StartTime:          time.Now(),
		RelayMode:          mode,
		RelayFormat:        format,
		IsStream:           stream,
		ShouldIncludeUsage: true,
		OriginModelName:    model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: model,
			ApiKey:            "test-api-key|eu-west-1",
			ChannelBaseUrl:    server.URL,
		},
	}
	switch format {
	case relaytypes.RelayFormatClaude:
		info.ShouldIncludeUsage = false
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	case relaytypes.RelayFormatOpenAIResponses:
		info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{}}
	}
	return info
}

// relayThroughBedrock runs one request through the adaptor as the relay does:
// convert, marshal, DoRequest, DoResponse.
func relayThroughBedrock(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, convert func(*Adaptor) (any, error)) (*dto.Usage, *relaytypes.NewAPIError) {
	t.Helper()
	adaptor := &Adaptor{}
	converted, err := convert(adaptor)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	response, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	require.Nil(t, response)
	usage, apiErr := adaptor.DoResponse(c, nil, info)
	if apiErr != nil {
		return nil, apiErr
	}
	return usage.(*dto.Usage), nil
}

func TestConvertOpenAIToConverse(t *testing.T) {
	tests := []struct {
		name    string
		request string
		want    string
		wantErr int
	}{
		{
			name:    "Nova: system, image, sampling and stop",
			request: `{"model":"eu.amazon.nova-lite-v1:0","messages":[{"role":"system","content":"Be brief."},{"role":"user","content":[{"type":"text","text":"What is this?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,` + tinyPNG + `"}}]}],"max_tokens":256,"temperature":0.2,"top_p":0.9,"stop":["END"]}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"What is this?"},{"image":{"format":"png","source":{"bytes":"` + tinyPNG + `"}}}]}],"system":[{"text":"Be brief."}],"inferenceConfig":{"maxTokens":256,"temperature":0.2,"topP":0.9,"stopSequences":["END"]}}`,
		},
		{
			name:    "GPT-oss: tool calls, merged tool results and reasoning effort",
			request: `{"model":"openai.gpt-oss-120b-1:0","reasoning_effort":"high","messages":[{"role":"user","content":"Weather in Paris and Rome?"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}},{"id":"call_2","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Rome\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"18C"},{"role":"tool","tool_call_id":"call_2","content":"24C"},{"role":"user","content":"Which is warmer?"}],"tools":[{"type":"function","function":{"name":"weather","description":"Current weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}],"tool_choice":"required"}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Weather in Paris and Rome?"}]},{"role":"assistant","content":[{"toolUse":{"toolUseId":"call_1","name":"weather","input":{"city":"Paris"}}},{"toolUse":{"toolUseId":"call_2","name":"weather","input":{"city":"Rome"}}}]},{"role":"user","content":[{"toolResult":{"toolUseId":"call_1","content":[{"text":"18C"}]}},{"toolResult":{"toolUseId":"call_2","content":[{"text":"24C"}]}},{"text":"Which is warmer?"}]}],"toolConfig":{"tools":[{"toolSpec":{"name":"weather","description":"Current weather","inputSchema":{"json":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}}],"toolChoice":{"any":{}}},"additionalModelRequestFields":{"reasoning_effort":"high"}}`,
		},
		{
			name:    "Voxtral: input_audio becomes an audio block",
			request: `{"model":"mistral.voxtral-small-24b-2507","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}},{"type":"text","text":"Transcribe"}]}]}`,
			want:    `{"messages":[{"role":"user","content":[{"audio":{"format":"wav","source":{"bytes":"UklGRg=="}}},{"text":"Transcribe"}]}]}`,
		},
		{
			name:    "Nova 2: cache point after a cached block, reasoning config, max_completion_tokens",
			request: `{"model":"global.amazon.nova-2-lite-v1:0","reasoning_effort":"low","max_completion_tokens":100,"messages":[{"role":"system","content":[{"type":"text","text":"Long manual","cache_control":{"type":"ephemeral"}}]},{"role":"user","content":"Q"}]}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Q"}]}],"system":[{"text":"Long manual"},{"cachePoint":{"type":"default"}}],"inferenceConfig":{"maxTokens":100},"additionalModelRequestFields":{"reasoningConfig":{"type":"enabled","maxReasoningEffort":"low"}}}`,
		},
		{
			name:    "Nova 2 at high effort: temperature and top_p are dropped, Bedrock refuses them",
			request: `{"model":"eu.amazon.nova-2-lite-v1:0","reasoning_effort":"high","temperature":0.5,"top_p":0.9,"messages":[{"role":"user","content":"Q"}]}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Q"}]}],"additionalModelRequestFields":{"reasoningConfig":{"type":"enabled","maxReasoningEffort":"high"}}}`,
		},
		{
			name:    "Nova 2 at high effort keeps max_tokens, the caller's cost cap",
			request: `{"model":"eu.amazon.nova-2-lite-v1:0","reasoning_effort":"xhigh","max_tokens":50,"temperature":0.5,"messages":[{"role":"user","content":"Q"}]}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Q"}]}],"inferenceConfig":{"maxTokens":50},"additionalModelRequestFields":{"reasoningConfig":{"type":"enabled","maxReasoningEffort":"high"}}}`,
		},
		{
			name:    "Mistral: a named tool choice; no reasoning field for a family without one",
			request: `{"model":"mistral.mistral-large-2402-v1:0","reasoning_effort":"medium","temperature":0,"messages":[{"role":"user","content":"Hi"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Hi"}]}],"inferenceConfig":{"temperature":0},"toolConfig":{"tools":[{"toolSpec":{"name":"lookup","inputSchema":{"json":{"type":"object","properties":{}}}}}],"toolChoice":{"tool":{"name":"lookup"}}}}`,
		},
		{
			name:    "n above 1 is refused",
			request: `{"model":"eu.meta.llama3-2-3b-instruct-v1:0","n":2,"messages":[{"role":"user","content":"Hi"}]}`,
			wantErr: http.StatusBadRequest,
		},
		{
			name:    "an unsupported image type is refused",
			request: `{"model":"google.gemma-3-4b-it","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/bmp;base64,Qk0="}}]}]}`,
			wantErr: http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(test.request), &request))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			converse, err := convertOpenAIToConverse(c, request.Model, &request)
			if test.wantErr != 0 {
				var apiErr *relaytypes.NewAPIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, test.wantErr, apiErr.StatusCode)
				assert.True(t, relaytypes.IsSkipRetryError(apiErr))
				return
			}
			require.NoError(t, err)
			body, err := common.Marshal(converse)
			require.NoError(t, err)
			assert.JSONEq(t, test.want, string(body))
		})
	}
}

const (
	converseTextReply      = `{"output":{"message":{"role":"assistant","content":[{"text":"Hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":3,"totalTokens":13},"metrics":{"latencyMs":100}}`
	converseReasoningReply = `{"output":{"message":{"role":"assistant","content":[{"reasoningContent":{"reasoningText":{"text":"Think first."}}},{"text":"Hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":3,"totalTokens":13}}`
	converseToolReply      = `{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"tooluse_1","name":"weather","input":{"city":"Paris"}}}]}},"stopReason":"tool_use","usage":{"inputTokens":10,"outputTokens":3,"totalTokens":13}}`
	converseCacheReply     = `{"output":{"message":{"role":"assistant","content":[{"text":"Hello"}]}},"stopReason":"max_tokens","usage":{"inputTokens":10,"outputTokens":3,"totalTokens":133,"cacheReadInputTokens":100,"cacheWriteInputTokens":20}}`
)

func TestConverseChatPerModelFamily(t *testing.T) {
	tests := []struct {
		family, model, reply, wantPath string
		wantContent, wantReasoning     string
		wantToolCall                   string
		wantFinish                     string
		wantPrompt, wantCached         int
	}{
		{family: "Amazon Nova (eu. profile)", model: "eu.amazon.nova-lite-v1:0", reply: converseCacheReply, wantPath: "/model/eu.amazon.nova-lite-v1%3A0/converse", wantContent: "Hello", wantFinish: "length", wantPrompt: 130, wantCached: 100},
		{family: "Amazon Nova short name resolves to the eu. profile", model: "nova-pro-v1:0", reply: converseTextReply, wantPath: "/model/eu.amazon.nova-pro-v1%3A0/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Amazon Nova 2 Lite", model: "eu.amazon.nova-2-lite-v1:0", reply: converseReasoningReply, wantPath: "/model/eu.amazon.nova-2-lite-v1%3A0/converse", wantContent: "Hello", wantReasoning: "Think first.", wantFinish: "stop", wantPrompt: 10},
		{family: "Meta Llama 3.2", model: "eu.meta.llama3-2-3b-instruct-v1:0", reply: converseTextReply, wantPath: "/model/eu.meta.llama3-2-3b-instruct-v1%3A0/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Mistral Large", model: "mistral.mistral-large-2402-v1:0", reply: converseTextReply, wantPath: "/model/mistral.mistral-large-2402-v1%3A0/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Mistral Pixtral", model: "eu.mistral.pixtral-large-2502-v1:0", reply: converseTextReply, wantPath: "/model/eu.mistral.pixtral-large-2502-v1%3A0/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Mistral Ministral", model: "mistral.ministral-3-8b-instruct", reply: converseToolReply, wantPath: "/model/mistral.ministral-3-8b-instruct/converse", wantToolCall: `{"city":"Paris"}`, wantFinish: "tool_calls", wantPrompt: 10},
		{family: "Mistral Devstral", model: "mistral.devstral-2-123b", reply: converseTextReply, wantPath: "/model/mistral.devstral-2-123b/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Mistral Magistral", model: "mistral.magistral-small-2509", reply: converseReasoningReply, wantPath: "/model/mistral.magistral-small-2509/converse", wantContent: "Hello", wantReasoning: "Think first.", wantFinish: "stop", wantPrompt: 10},
		{family: "Mistral Voxtral", model: "mistral.voxtral-mini-3b-2507", reply: converseTextReply, wantPath: "/model/mistral.voxtral-mini-3b-2507/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Google Gemma 3", model: "google.gemma-3-27b-it", reply: converseTextReply, wantPath: "/model/google.gemma-3-27b-it/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "Qwen3", model: "qwen.qwen3-32b-v1:0", reply: converseToolReply, wantPath: "/model/qwen.qwen3-32b-v1%3A0/converse", wantToolCall: `{"city":"Paris"}`, wantFinish: "tool_calls", wantPrompt: 10},
		{family: "Qwen3 VL", model: "qwen.qwen3-vl-235b-a22b", reply: converseTextReply, wantPath: "/model/qwen.qwen3-vl-235b-a22b/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "OpenAI GPT-oss", model: "openai.gpt-oss-120b-1:0", reply: converseReasoningReply, wantPath: "/model/openai.gpt-oss-120b-1%3A0/converse", wantContent: "Hello", wantReasoning: "Think first.", wantFinish: "stop", wantPrompt: 10},
		{family: "MiniMax", model: "minimax.minimax-m2.5", reply: converseReasoningReply, wantPath: "/model/minimax.minimax-m2.5/converse", wantContent: "Hello", wantReasoning: "Think first.", wantFinish: "stop", wantPrompt: 10},
		{family: "Z.ai GLM", model: "zai.glm-4.7-flash", reply: converseTextReply, wantPath: "/model/zai.glm-4.7-flash/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
		{family: "NVIDIA Nemotron", model: "nvidia.nemotron-nano-9b-v2", reply: converseTextReply, wantPath: "/model/nvidia.nemotron-nano-9b-v2/converse", wantContent: "Hello", wantFinish: "stop", wantPrompt: 10},
	}
	for _, test := range tests {
		t.Run(test.family, func(t *testing.T) {
			server, captured := startBedrockStub(t, jsonFixture(test.reply))
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, test.model, false)
			request := &dto.GeneralOpenAIRequest{Model: test.model, Messages: []dto.Message{{Role: "user", Content: "Hi"}}}

			usage, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
			require.Nil(t, apiErr)

			requests := captured()
			require.Len(t, requests, 1)
			assert.Equal(t, test.wantPath, requests[0].path)
			assert.Equal(t, "Bearer test-api-key", requests[0].header.Get("Authorization"))
			assert.JSONEq(t, `{"messages":[{"role":"user","content":[{"text":"Hi"}]}]}`, string(requests[0].body))

			body := recorder.Body.Bytes()
			assert.Equal(t, test.wantContent, ojson.GetBytes(body, "choices.0.message.content").String())
			assert.Equal(t, test.wantReasoning, ojson.GetBytes(body, "choices.0.message.reasoning_content").String())
			assert.Equal(t, test.wantToolCall, ojson.GetBytes(body, "choices.0.message.tool_calls.0.function.arguments").String())
			assert.Equal(t, test.wantFinish, ojson.GetBytes(body, "choices.0.finish_reason").String())
			assert.Equal(t, test.wantPrompt, usage.PromptTokens)
			assert.Equal(t, 3, usage.CompletionTokens)
			assert.Equal(t, test.wantCached, usage.PromptTokensDetails.CachedTokens)
			assert.EqualValues(t, test.wantPrompt, ojson.GetBytes(body, "usage.prompt_tokens").Int())
		})
	}
}

func TestConverseSignsWithSigV4ForAccessKeys(t *testing.T) {
	server, captured := startBedrockStub(t, jsonFixture(converseTextReply))
	c := newAwsTestContext(httptest.NewRecorder(), context.Background())
	info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, "eu.amazon.nova-lite-v1:0", false)
	info.ApiKey = "access-key|secret-key|eu-west-1"
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}}
	_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
	require.Nil(t, apiErr)

	wire := captured()[0]
	authorization := wire.header.Get("Authorization")
	require.True(t, strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 Credential=access-key/"))
	assert.Contains(t, authorization, "/eu-west-1/bedrock/aws4_request")
	// Re-sign what reached the server, so the signature covers the escaped path.
	verification := httptest.NewRequest(http.MethodPost, server.URL+wire.path, bytes.NewReader(wire.body))
	verification.Header = http.Header{}
	_, signedHeaders, _ := strings.Cut(authorization, "SignedHeaders=")
	signedHeaders, _, _ = strings.Cut(signedHeaders, ",")
	for name := range strings.SplitSeq(signedHeaders, ";") {
		if name != "host" {
			verification.Header[http.CanonicalHeaderKey(name)] = wire.header.Values(name)
		}
	}
	signedAt, err := time.Parse("20060102T150405Z", wire.header.Get("X-Amz-Date"))
	require.NoError(t, err)
	hash := sha256.Sum256(wire.body)
	require.NoError(t, v4.NewSigner().SignHTTP(context.Background(), aws.Credentials{AccessKeyID: "access-key", SecretAccessKey: "secret-key"}, verification, hex.EncodeToString(hash[:]), "bedrock", "eu-west-1", signedAt))
	assert.Equal(t, authorization, verification.Header.Get("Authorization"))
}

func converseStreamEvents(t *testing.T) bedrockReplyFixture {
	return converseStreamFixture(t,
		[2]string{"messageStart", `{"role":"assistant"}`},
		[2]string{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":"Think."}}}`},
		[2]string{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"text":"Hel"}}`},
		[2]string{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"text":"lo"}}`},
		[2]string{"contentBlockStop", `{"contentBlockIndex":1}`},
		[2]string{"contentBlockStart", `{"contentBlockIndex":2,"start":{"toolUse":{"toolUseId":"tooluse_1","name":"weather"}}}`},
		[2]string{"contentBlockDelta", `{"contentBlockIndex":2,"delta":{"toolUse":{"input":"{\"city\":"}}}`},
		[2]string{"contentBlockDelta", `{"contentBlockIndex":2,"delta":{"toolUse":{"input":"\"Paris\"}"}}}`},
		[2]string{"contentBlockStop", `{"contentBlockIndex":2}`},
		[2]string{"messageStop", `{"stopReason":"tool_use"}`},
		[2]string{"metadata", `{"usage":{"inputTokens":12,"outputTokens":7,"totalTokens":59,"cacheReadInputTokens":40},"metrics":{"latencyMs":5}}`},
	)
}

func TestConverseStreamPerClientProtocol(t *testing.T) {
	originalTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
	tests := []struct {
		name     string
		mode     int
		format   relaytypes.RelayFormat
		model    string
		convert  func(*Adaptor, *gin.Context, *relaycommon.RelayInfo) (any, error)
		wantPath string
		wantSSE  []string
	}{
		{
			name: "chat completions", mode: relayconstant.RelayModeChatCompletions, format: relaytypes.RelayFormatOpenAI, model: "openai.gpt-oss-20b-1:0",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}})
			},
			wantPath: "/model/openai.gpt-oss-20b-1%3A0/converse-stream",
			wantSSE:  []string{`"reasoning_content":"Think."`, `"content":"Hel"`, `"content":"lo"`, `"name":"weather"`, `"arguments":"\"Paris\"}"`, `"finish_reason":"tool_calls"`, `"prompt_tokens":52`, `"cached_tokens":40`, "data: [DONE]"},
		},
		{
			name: "Anthropic Messages for a model other than Claude", mode: relayconstant.RelayModeUnknown, format: relaytypes.RelayFormatClaude, model: "qwen.qwen3-32b-v1:0",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.ClaudeRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"qwen.qwen3-32b-v1:0","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`), &request))
				return a.ConvertClaudeRequest(c, info, &request)
			},
			wantPath: "/model/qwen.qwen3-32b-v1%3A0/converse-stream",
			wantSSE:  []string{"event: message_start", `"text":"Hel"`, `"type":"tool_use"`, `"stop_reason":"tool_use"`, "event: message_stop"},
		},
		{
			name: "Responses, Claude included", mode: relayconstant.RelayModeResponses, format: relaytypes.RelayFormatOpenAIResponses, model: "eu.anthropic.claude-sonnet-4-6",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.anthropic.claude-sonnet-4-6","input":"Hi","stream":true}`), &request))
				return a.ConvertOpenAIResponsesRequest(c, info, request)
			},
			wantPath: "/model/eu.anthropic.claude-sonnet-4-6/converse-stream",
			wantSSE:  []string{"response.created", `"delta":"Hel"`, "response.function_call_arguments", "response.completed"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, captured := startBedrockStub(t, converseStreamEvents(t))
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			info := newBedrockTestInfo(server, test.mode, test.format, test.model, true)

			usage, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return test.convert(a, c, info) })
			require.Nil(t, apiErr)

			requests := captured()
			require.Len(t, requests, 1)
			assert.Equal(t, test.wantPath, requests[0].path)
			assert.Equal(t, "application/vnd.amazon.eventstream", requests[0].header.Get("Accept"))
			sse := recorder.Body.String()
			for _, want := range test.wantSSE {
				assert.Contains(t, sse, want)
			}
			assert.Equal(t, 52, usage.PromptTokens)
			assert.Equal(t, 7, usage.CompletionTokens)
			cached := usage.PromptTokensDetails.CachedTokens
			if usage.InputTokensDetails != nil {
				cached = max(cached, usage.InputTokensDetails.CachedTokens)
			}
			assert.Equal(t, 40, cached)
		})
	}
}

func TestBedrockErrorsKeepTheirMeaning(t *testing.T) {
	tests := []struct {
		name          string
		fixture       func(*testing.T) bedrockReplyFixture
		stream        bool
		wantStatus    int
		wantCode      string
		wantSkipRetry bool
	}{
		{name: "throttling", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 429, header: map[string]string{"X-Amzn-ErrorType": "ThrottlingException:http://internal.amazon.com/coral/com.amazon.bedrock/"}, body: []byte(`{"message":"Too many requests, please wait before trying again."}`)}
		}, wantStatus: 429, wantCode: "rate_limit_exceeded"},
		{name: "validation", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 400, header: map[string]string{"X-Amzn-ErrorType": "ValidationException"}, body: []byte(`{"message":"messages: text field is blank"}`)}
		}, wantStatus: 400, wantCode: "invalid_request", wantSkipRetry: true},
		{name: "unknown model id", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 400, header: map[string]string{"X-Amzn-ErrorType": "ValidationException"}, body: []byte(`{"message":"The provided model identifier is invalid."}`)}
		}, wantStatus: 404, wantCode: "model_not_found"},
		{name: "access denied", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 403, header: map[string]string{"X-Amzn-ErrorType": "AccessDeniedException"}, body: []byte(`{"message":"You don't have access to the model with the specified model ID."}`)}
		}, wantStatus: 403, wantCode: "access_denied"},
		{name: "model not ready", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 429, header: map[string]string{"X-Amzn-ErrorType": "ModelNotReadyException"}, body: []byte(`{"message":"Model is not ready for inference."}`)}
		}, wantStatus: 503, wantCode: "model_not_ready"},
		{name: "model timeout", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 408, header: map[string]string{"X-Amzn-ErrorType": "ModelTimeoutException"}, body: []byte(`{"message":"Model has timed out in processing the request."}`)}
		}, wantStatus: 504, wantCode: "model_timeout"},
		{name: "invalid API key", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 403, header: map[string]string{"X-Amzn-ErrorType": "UnrecognizedClientException"}, body: []byte(`{"Message":"The security token included in the request is invalid."}`)}
		}, wantStatus: 401, wantCode: "invalid_api_key"},
		{name: "unknown exception keeps the status", fixture: func(*testing.T) bedrockReplyFixture {
			return bedrockReplyFixture{status: 409, header: map[string]string{"X-Amzn-ErrorType": "ConflictException"}, body: []byte(`{"message":"conflict"}`)}
		}, wantStatus: 409, wantCode: "bedrock_error"},
		{name: "throttling as the first stream event", stream: true, fixture: func(t *testing.T) bedrockReplyFixture {
			return converseStreamFixture(t, [2]string{"!throttlingException", `{"message":"Too many tokens"}`})
		}, wantStatus: 429, wantCode: "rate_limit_exceeded"},
		{name: "service unavailable as the first stream event", stream: true, fixture: func(t *testing.T) bedrockReplyFixture {
			return converseStreamFixture(t, [2]string{"!serviceUnavailableException", `{"message":"try later"}`})
		}, wantStatus: 503, wantCode: "service_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := startBedrockStub(t, test.fixture(t))
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, "eu.amazon.nova-lite-v1:0", test.stream)
			request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}}
			_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
			require.NotNil(t, apiErr)
			assert.Equal(t, test.wantStatus, apiErr.StatusCode)
			assert.Equal(t, test.wantCode, string(apiErr.GetErrorCode()))
			assert.Equal(t, test.wantSkipRetry, relaytypes.IsSkipRetryError(apiErr))
			assert.Empty(t, recorder.Body.String(), "nothing reaches the client before the error")
		})
	}

	t.Run("SDK error on the Claude InvokeModel path", func(t *testing.T) {
		err := &smithy.OperationError{ServiceID: "Bedrock Runtime", OperationName: "InvokeModel", Err: &smithy.GenericAPIError{Code: "ThrottlingException", Message: "Too many requests"}}
		apiErr := newAwsInvokeError(context.Background(), err, "InvokeModel")
		assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
		assert.Equal(t, "rate_limit_exceeded", string(apiErr.GetErrorCode()))
	})
}

func TestBedrockEmbeddings(t *testing.T) {
	tokens := map[string]string{bedrockInputTokenHeader: "4"}
	tests := []struct {
		family    string
		model     string
		request   string
		replies   []bedrockReplyFixture
		wantPath  string
		wantBody  []string
		wantFirst []float64
		wantCount int
		wantUsage int
	}{
		{
			family: "Titan Text v2: one call per input, dimensions", model: "amazon.titan-embed-text-v2:0", request: `{"input":["a","b"],"dimensions":256}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"embedding":[1,2]}`)}, {header: tokens, body: []byte(`{"embedding":[3,4]}`)}},
			wantPath: "/model/amazon.titan-embed-text-v2%3A0/invoke", wantBody: []string{`{"inputText":"a","dimensions":256}`, `{"inputText":"b","dimensions":256}`},
			wantFirst: []float64{1, 2}, wantCount: 2, wantUsage: 8,
		},
		{
			family: "Titan Multimodal: an image data URL", model: "amazon.titan-embed-image-v1", request: `{"input":"data:image/png;base64,aW1n","dimensions":384}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"embedding":[0.5],"inputTextTokenCount":0}`)}},
			wantPath: "/model/amazon.titan-embed-image-v1/invoke", wantBody: []string{`{"inputImage":"aW1n","embeddingConfig":{"outputEmbeddingLength":384}}`},
			wantFirst: []float64{0.5}, wantCount: 1, wantUsage: 4,
		},
		{
			family: "Cohere Embed v3: one batched call", model: "cohere.embed-multilingual-v3", request: `{"input":["a","b"]}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"id":"x","response_type":"embeddings_by_type","embeddings":{"float":[[1,1],[2,2]]},"texts":["a","b"]}`)}},
			wantPath: "/model/cohere.embed-multilingual-v3/invoke", wantBody: []string{`{"texts":["a","b"],"input_type":"search_document","embedding_types":["float"]}`},
			wantFirst: []float64{1, 1}, wantCount: 2, wantUsage: 4,
		},
		{
			family: "Cohere Embed v4: output dimension, float array answer", model: "eu.cohere.embed-v4:0", request: `{"input":"a","dimensions":512}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"id":"x","response_type":"embeddings_floats","embeddings":[[7,8]]}`)}},
			wantPath: "/model/eu.cohere.embed-v4%3A0/invoke", wantBody: []string{`{"texts":["a"],"input_type":"search_document","embedding_types":["float"],"output_dimension":512}`},
			wantFirst: []float64{7, 8}, wantCount: 1, wantUsage: 4,
		},
	}
	for _, test := range tests {
		t.Run(test.family, func(t *testing.T) {
			server, captured := startBedrockStub(t, test.replies...)
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeEmbeddings, relaytypes.RelayFormatEmbedding, test.model, false)
			var request dto.EmbeddingRequest
			require.NoError(t, common.Unmarshal([]byte(test.request), &request))

			usage, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertEmbeddingRequest(c, info, request) })
			require.Nil(t, apiErr)

			requests := captured()
			require.Len(t, requests, len(test.wantBody))
			for i, want := range test.wantBody {
				assert.Equal(t, test.wantPath, requests[i].path)
				assert.JSONEq(t, want, string(requests[i].body))
			}
			var response embeddingResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Len(t, response.Data, test.wantCount)
			first, ok := response.Data[0].Embedding.([]any)
			require.True(t, ok)
			for i, value := range test.wantFirst {
				assert.InDelta(t, value, first[i], 1e-9)
			}
			assert.Equal(t, test.wantUsage, usage.PromptTokens)
			assert.EqualValues(t, test.wantUsage, response.Usage.PromptTokens)
		})
	}

	t.Run("base64 encoding_format is little-endian float32", func(t *testing.T) {
		server, _ := startBedrockStub(t, bedrockReplyFixture{header: tokens, body: []byte(`{"embedding":[1.5,-2]}`)})
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		info := newBedrockTestInfo(server, relayconstant.RelayModeEmbeddings, relaytypes.RelayFormatEmbedding, "amazon.titan-embed-text-v2:0", false)
		_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) {
			return a.ConvertEmbeddingRequest(c, info, dto.EmbeddingRequest{Input: "a", EncodingFormat: "base64"})
		})
		require.Nil(t, apiErr)
		encoded := ojson.GetBytes(recorder.Body.Bytes(), "data.0.embedding").String()
		raw, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		require.Len(t, raw, 8)
		assert.Equal(t, float32(1.5), math.Float32frombits(binary.LittleEndian.Uint32(raw[0:])))
		assert.Equal(t, float32(-2), math.Float32frombits(binary.LittleEndian.Uint32(raw[4:])))
	})

	t.Run("a chat model is refused", func(t *testing.T) {
		_, err := convertEmbeddingRequest("eu.amazon.nova-lite-v1:0", dto.EmbeddingRequest{Input: "a"})
		var apiErr *relaytypes.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	})
}

func TestBedrockRerankIsRefused(t *testing.T) {
	_, err := (&Adaptor{}).ConvertRerankRequest(nil, relayconstant.RelayModeRerank, dto.RerankRequest{})
	var apiErr *relaytypes.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.True(t, relaytypes.IsSkipRetryError(apiErr))
}

// onDemandRefusal is Bedrock's answer for a model that a region serves only
// through an inference profile, as eu-west-1 gave it for Nova 2 Lite.
func onDemandRefusal(modelID string) bedrockReplyFixture {
	return bedrockReplyFixture{status: 400, header: map[string]string{"X-Amzn-ErrorType": "ValidationException:http://internal.amazon.com/coral/com.amazon.bedrock/"}, body: []byte(`{"message":"Invocation of model ID ` + modelID + ` with on-demand throughput isn’t supported. Retry your request with the ID or ARN of an inference profile that contains this model."}`)}
}

func TestBedrockInferenceProfiles(t *testing.T) {
	originalTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })

	t.Run("models eu-west-1 serves only through the eu. profile resolve to it", func(t *testing.T) {
		for _, model := range []string{
			"amazon.nova-2-lite-v1:0", "amazon.nova-lite-v1:0", "amazon.nova-micro-v1:0", "amazon.nova-pro-v1:0",
			"anthropic.claude-haiku-4-5-20251001-v1:0", "anthropic.claude-opus-4-5-20251101-v1:0", "anthropic.claude-opus-4-6-v1",
			"anthropic.claude-opus-4-7", "anthropic.claude-opus-4-8", "anthropic.claude-opus-5",
			"anthropic.claude-sonnet-4-5-20250929-v1:0", "anthropic.claude-sonnet-4-6", "anthropic.claude-sonnet-5",
			"mistral.pixtral-large-2502-v1:0",
		} {
			assert.Equal(t, "eu."+model, resolveBedrockModelID(0, model, "eu-west-1"), model)
		}
	})

	t.Run("an id that already names a profile is not touched", func(t *testing.T) {
		for _, model := range []string{
			"eu.amazon.nova-2-lite-v1:0", "us.amazon.nova-2-lite-v1:0", "apac.amazon.nova-2-lite-v1:0", "global.amazon.nova-2-lite-v1:0",
			"arn:aws:bedrock:eu-west-1:123456789012:inference-profile/eu.amazon.nova-2-lite-v1:0",
		} {
			assert.Equal(t, model, resolveBedrockModelID(0, model, "eu-west-1"), model)
			server, captured := startBedrockStub(t, onDemandRefusal(model))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, model, false)
			request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}}
			_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
			require.NotNil(t, apiErr, model)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Len(t, captured(), 1, model)
		}
	})

	tests := []struct {
		name       string
		channelID  int
		stream     bool
		fixtures   func(*testing.T) []bedrockReplyFixture
		wantPaths  []string
		wantStatus int
		wantNext   string
	}{
		{
			name: "a refusal on demand is retried once through the eu. profile and remembered", channelID: 9101,
			fixtures: func(*testing.T) []bedrockReplyFixture {
				return []bedrockReplyFixture{onDemandRefusal("amazon.nova-9-test-v1:0"), jsonFixture(converseTextReply)}
			},
			wantPaths: []string{"/model/amazon.nova-9-test-v1%3A0/converse", "/model/eu.amazon.nova-9-test-v1%3A0/converse"},
			wantNext:  "/model/eu.amazon.nova-9-test-v1%3A0/converse",
		},
		{
			name: "a streaming request is retried before anything reaches the client", channelID: 9102, stream: true,
			fixtures: func(t *testing.T) []bedrockReplyFixture {
				return []bedrockReplyFixture{onDemandRefusal("amazon.nova-9-test-v1:0"), converseStreamEvents(t)}
			},
			wantPaths: []string{"/model/amazon.nova-9-test-v1%3A0/converse-stream", "/model/eu.amazon.nova-9-test-v1%3A0/converse-stream"},
			wantNext:  "/model/eu.amazon.nova-9-test-v1%3A0/converse-stream",
		},
		{
			name: "another validation error is not retried", channelID: 9103,
			fixtures: func(*testing.T) []bedrockReplyFixture {
				return []bedrockReplyFixture{{status: 400, header: map[string]string{"X-Amzn-ErrorType": "ValidationException"}, body: []byte(`{"message":"messages: text field is blank"}`)}}
			},
			wantPaths:  []string{"/model/amazon.nova-9-test-v1%3A0/converse"},
			wantStatus: http.StatusBadRequest,
			wantNext:   "/model/amazon.nova-9-test-v1%3A0/converse",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const model = "amazon.nova-9-test-v1:0"
			server, captured := startBedrockStub(t, test.fixtures(t)...)
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, model, test.stream)
			info.ChannelId = test.channelID
			request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}}

			usage, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
			var paths []string
			for _, r := range captured() {
				paths = append(paths, r.path)
			}
			assert.Equal(t, test.wantPaths, paths)
			if test.wantStatus != 0 {
				require.NotNil(t, apiErr)
				assert.Equal(t, test.wantStatus, apiErr.StatusCode)
			} else {
				require.Nil(t, apiErr)
				assert.Positive(t, usage.CompletionTokens)
				assert.Contains(t, recorder.Body.String(), `"model":"`+model+`"`, "the answer names the catalog model, not the profile")
			}
			assert.Equal(t, model, info.UpstreamModelName, "usage keeps the catalog model")

			next, nextCaptured := startBedrockStub(t, jsonFixture(converseTextReply))
			c = newAwsTestContext(httptest.NewRecorder(), context.Background())
			info = newBedrockTestInfo(next, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, model, false)
			info.ChannelId = test.channelID
			if test.stream {
				test.wantNext = strings.TrimSuffix(test.wantNext, "-stream")
			}
			_, apiErr = relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
			require.Nil(t, apiErr)
			require.Len(t, nextCaptured(), 1)
			assert.Equal(t, test.wantNext, nextCaptured()[0].path, "the next call on the channel")
		})
	}

	t.Run("a region without a geographic profile never falls back to global.", func(t *testing.T) {
		for _, region := range []string{"sa-east-1", "me-central-1"} {
			server, captured := startBedrockStub(t, onDemandRefusal("amazon.nova-9-test-v1:0"))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, "amazon.nova-9-test-v1:0", false)
			info.ApiKey = "test-api-key|" + region
			info.ChannelId = 9104
			request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}}
			_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIRequest(c, info, request) })
			require.NotNil(t, apiErr)
			requests := captured()
			require.Len(t, requests, 1, region)
			assert.Equal(t, "/model/amazon.nova-9-test-v1%3A0/converse", requests[0].path)
			assert.Equal(t, "amazon.nova-9-test-v1:0", resolveBedrockModelID(9104, "amazon.nova-9-test-v1:0", region))
		}
	})

	t.Run("Claude on InvokeModel is retried through the eu. profile", func(t *testing.T) {
		const model = "anthropic.claude-test-profile-only"
		var paths []string
		client := bedrockruntime.New(bedrockruntime.Options{
			Region:       "eu-west-1",
			BaseEndpoint: aws.String("https://bedrock.test"),
			Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("access-key", "secret-key", "")),
			Retryer:      aws.NopRetryer{},
			HTTPClient: awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
				paths = append(paths, request.URL.EscapedPath())
				if len(paths) == 1 {
					refusal := onDemandRefusal(model)
					return &http.Response{StatusCode: refusal.status, Header: http.Header{"Content-Type": {"application/json"}, "X-Amzn-Errortype": {refusal.header["X-Amzn-ErrorType"]}}, Body: io.NopCloser(bytes.NewReader(refusal.body)), Request: request}, nil
				}
				body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			}),
		})
		input := &bedrockruntime.InvokeModelInput{ModelId: aws.String(model), Body: []byte(`{}`), Accept: aws.String("application/json"), ContentType: aws.String("application/json")}
		adaptor := &Adaptor{AwsClient: client, AwsReq: input}
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		info := newAwsTestRelayInfo()
		info.IsStream = false
		info.ChannelId = 9105
		info.UpstreamModelName = model

		apiErr, usage := awsHandler(c, info, adaptor)
		require.Nil(t, apiErr)
		assert.Equal(t, []string{"/model/" + model + "/invoke", "/model/eu." + model + "/invoke"}, paths)
		assert.Equal(t, 2, usage.CompletionTokens)
		assert.Equal(t, model, info.UpstreamModelName)
		assert.Equal(t, "eu."+model, resolveBedrockModelID(9105, model, "eu-west-1"))
	})
}
