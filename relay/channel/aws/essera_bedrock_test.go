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
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/metering"
	"github.com/QuantumNous/new-api/essera/rerankunits"
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
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
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
			want:    `{"messages":[{"role":"user","content":[{"text":"Weather in Paris and Rome?"}]},{"role":"assistant","content":[{"toolUse":{"toolUseId":"call_1","name":"weather","input":{"city":"Paris"}}},{"toolUse":{"toolUseId":"call_2","name":"weather","input":{"city":"Rome"}}}]},{"role":"user","content":[{"toolResult":{"toolUseId":"call_1","content":[{"text":"18C"}]}},{"toolResult":{"toolUseId":"call_2","content":[{"text":"24C"}]}},{"text":"Which is warmer?"}]}],"toolConfig":{"tools":[{"toolSpec":{"name":"weather","description":"Current weather","inputSchema":{"json":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}}],"toolChoice":{"any":{}}},"additionalModelRequestFields":{"reasoning":{"effort":"high"}}}`,
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
			name:    "Nova 2 at high effort keeps stop sequences",
			request: `{"model":"eu.amazon.nova-2-lite-v1:0","reasoning_effort":"high","temperature":0.5,"stop":["END"],"messages":[{"role":"user","content":"Q"}]}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Q"}]}],"inferenceConfig":{"stopSequences":["END"]},"additionalModelRequestFields":{"reasoningConfig":{"type":"enabled","maxReasoningEffort":"high"}}}`,
		},
		{
			name:    "Nova 2 at medium effort keeps max_tokens and sampling",
			request: `{"model":"eu.amazon.nova-2-lite-v1:0","reasoning_effort":"medium","max_tokens":50,"temperature":0.5,"messages":[{"role":"user","content":"Q"}]}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Q"}]}],"inferenceConfig":{"maxTokens":50,"temperature":0.5},"additionalModelRequestFields":{"reasoningConfig":{"type":"enabled","maxReasoningEffort":"medium"}}}`,
		},
		{
			name:    "Mistral: a named tool choice; no reasoning field for a family without one",
			request: `{"model":"mistral.mistral-large-2402-v1:0","reasoning_effort":"medium","temperature":0,"messages":[{"role":"user","content":"Hi"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
			want:    `{"messages":[{"role":"user","content":[{"text":"Hi"}]}],"inferenceConfig":{"temperature":0},"toolConfig":{"tools":[{"toolSpec":{"name":"lookup","inputSchema":{"json":{"type":"object","properties":{}}}}}],"toolChoice":{"tool":{"name":"lookup"}}}}`,
		},
		{
			name:    "Nova: a file part with file_data becomes a named document block",
			request: `{"model":"eu.amazon.nova-pro-v1:0","messages":[{"role":"user","content":[{"type":"file","file":{"filename":"Q3 report.pdf","file_data":"data:application/pdf;base64,JVBERi0="}},{"type":"text","text":"Summarise"}]}]}`,
			want:    `{"messages":[{"role":"user","content":[{"document":{"format":"pdf","name":"Q3 report pdf","source":{"bytes":"JVBERi0="}}},{"text":"Summarise"}]}]}`,
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

// Nova 2 at high effort refuses maxTokens. The Gateway refuses the request
// itself, in every client protocol, rather than drop the caller's cost cap.
func TestNova2HighEffortRefusesMaxTokens(t *testing.T) {
	const message = "Nova 2 at high reasoning effort does not accept max_tokens: remove max_tokens or use medium reasoning effort."
	tests := []struct {
		name    string
		format  relaytypes.RelayFormat
		mode    int
		convert func(*Adaptor, *gin.Context, *relaycommon.RelayInfo) (any, error)
	}{
		{
			name: "chat completions, max_tokens", format: relaytypes.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","reasoning_effort":"high","max_tokens":50,"messages":[{"role":"user","content":"Q"}]}`), &request))
				return a.ConvertOpenAIRequest(c, info, &request)
			},
		},
		{
			name: "chat completions, max_completion_tokens at xhigh", format: relaytypes.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","reasoning":{"effort":"xhigh"},"max_completion_tokens":50,"messages":[{"role":"user","content":"Q"}]}`), &request))
				return a.ConvertOpenAIRequest(c, info, &request)
			},
		},
		{
			name: "Anthropic Messages, whose max_tokens is mandatory", format: relaytypes.RelayFormatClaude, mode: relayconstant.RelayModeUnknown,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.ClaudeRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","max_tokens":50,"output_config":{"effort":"high"},"messages":[{"role":"user","content":"Q"}]}`), &request))
				return a.ConvertClaudeRequest(c, info, &request)
			},
		},
		{
			name: "Responses, max_output_tokens", format: relaytypes.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","input":"Q","reasoning":{"effort":"high"},"max_output_tokens":50}`), &request))
				return a.ConvertOpenAIResponsesRequest(c, info, request)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, captured := startBedrockStub(t, jsonFixture(converseTextReply))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			info := newBedrockTestInfo(server, test.mode, test.format, "eu.amazon.nova-2-lite-v1:0", false)

			_, err := test.convert(&Adaptor{}, c, info)

			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.True(t, relaytypes.IsSkipRetryError(apiErr))
			openAIError := apiErr.ToOpenAIError()
			assert.Equal(t, "invalid_request_error", openAIError.Type)
			assert.Equal(t, message, openAIError.Message)
			claudeError := apiErr.ToClaudeError()
			assert.Equal(t, "invalid_request_error", claudeError.Type)
			assert.Equal(t, message, claudeError.Message)
			assert.Empty(t, captured(), "Bedrock is not called")
		})
	}

	t.Run("Responses at medium effort keeps max_output_tokens", func(t *testing.T) {
		server, _ := startBedrockStub(t, jsonFixture(converseTextReply))
		c := newAwsTestContext(httptest.NewRecorder(), context.Background())
		info := newBedrockTestInfo(server, relayconstant.RelayModeResponses, relaytypes.RelayFormatOpenAIResponses, "eu.amazon.nova-2-lite-v1:0", false)
		var request dto.OpenAIResponsesRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","input":"Q","reasoning":{"effort":"medium"},"max_output_tokens":50}`), &request))

		converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, request)

		require.NoError(t, err)
		converse := converted.(*converseRequest)
		require.NotNil(t, converse.InferenceConfig)
		assert.Equal(t, uint(50), *converse.InferenceConfig.MaxTokens)
		assert.Equal(t, map[string]any{"reasoningConfig": map[string]any{"type": "enabled", "maxReasoningEffort": "medium"}}, converse.AdditionalModelRequestFields)
	})
}

// Images and documents over Bedrock's limits are refused before Bedrock is
// called, on Converse and on Claude's InvokeModel alike.
func TestBedrockMediaLimits(t *testing.T) {
	image := func(bytes int) string {
		return `{"type":"image_url","image_url":{"url":"data:image/png;base64,` + strings.Repeat("A", bytes/3*4) + `"}}`
	}
	document := func(bytes int) string {
		return `{"type":"file","file":{"filename":"a.pdf","file_data":"data:application/pdf;base64,` + strings.Repeat("A", bytes/3*4) + `"}}`
	}
	repeat := func(part string, n int) string {
		return strings.TrimSuffix(strings.Repeat(part+",", n), ",")
	}
	tests := []struct {
		name    string
		parts   string
		wantErr string
	}{
		{name: "an image of 3.75 MB passes", parts: image(bedrockMaxImageBytes)},
		{name: "an image over 3.75 MB is refused", parts: image(bedrockMaxImageBytes + 3), wantErr: "image 1 is 3.8 MB; Bedrock accepts images of up to 3.75 MB"},
		{name: "20 images pass", parts: repeat(image(3), 20)},
		{name: "21 images are refused", parts: repeat(image(3), 21), wantErr: "Bedrock accepts at most 20 images in a request"},
		{name: "a document of 4.5 MB passes", parts: document(bedrockMaxDocumentBytes)},
		{name: "a document over 4.5 MB is refused", parts: document(bedrockMaxDocumentBytes + 3), wantErr: "document 1 is 4.5 MB; Bedrock accepts documents of up to 4.5 MB"},
		{name: "5 documents pass", parts: repeat(document(3), 5)},
		{name: "6 documents are refused", parts: repeat(document(3), 6), wantErr: "Bedrock accepts at most 5 documents in a request"},
		{name: "a linked image is counted, not measured", parts: `{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(`{"model":"m","messages":[{"role":"user","content":[`+test.parts+`]}]}`), &request))
			err := checkBedrockMediaLimits(request.Messages)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.True(t, relaytypes.IsSkipRetryError(apiErr))
			assert.Equal(t, test.wantErr, apiErr.ToOpenAIError().Message)
		})
	}

	t.Run("the adaptor refuses before calling Bedrock, on either route", func(t *testing.T) {
		for _, model := range []string{"eu.amazon.nova-pro-v1:0", "eu.anthropic.claude-sonnet-4-5-20250929-v1:0"} {
			server, captured := startBedrockStub(t, jsonFixture(converseTextReply))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, model, false)
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(`{"model":"`+model+`","messages":[{"role":"user","content":[`+repeat(document(3), 6)+`]}]}`), &request))

			_, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)

			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr, model)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode, model)
			assert.Empty(t, captured(), model)
		}
	})
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

func TestConverseRecordsTheUnitsBedrockBilled(t *testing.T) {
	originalTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
	const byTTL = `{"inputTokens":10,"outputTokens":3,"totalTokens":163,"cacheReadInputTokens":100,"cacheWriteInputTokens":50,"cacheDetails":[{"inputTokens":30,"ttl":"1h"},{"inputTokens":20,"ttl":"5m"}]}`
	const noTTL = `{"inputTokens":10,"outputTokens":3,"totalTokens":133,"cacheReadInputTokens":100,"cacheWriteInputTokens":20}`
	reply := func(usage string) bedrockReplyFixture {
		return jsonFixture(`{"output":{"message":{"role":"assistant","content":[{"text":"Hello"}]}},"stopReason":"end_turn","usage":` + usage + `}`)
	}
	streamed := func(usage string) func(*testing.T) bedrockReplyFixture {
		return func(t *testing.T) bedrockReplyFixture {
			return converseStreamFixture(t,
				[2]string{"messageStart", `{"role":"assistant"}`},
				[2]string{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Hello"}}`},
				[2]string{"messageStop", `{"stopReason":"end_turn"}`},
				[2]string{"metadata", `{"usage":` + usage + `}`},
			)
		}
	}
	chat := func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
		return a.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "Hi"}}})
	}
	messages := func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
		var request dto.ClaudeRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"qwen.qwen3-32b-v1:0","max_tokens":64,"messages":[{"role":"user","content":"Hi"}]}`), &request))
		return a.ConvertClaudeRequest(c, info, &request)
	}
	responses := func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
		var request dto.OpenAIResponsesRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.anthropic.claude-sonnet-4-6","input":"Hi"}`), &request))
		return a.ConvertOpenAIResponsesRequest(c, info, request)
	}
	split := metering.Units{Input: 10, CacheRead: 100, CacheWrite: 20, CacheWrite1h: 30, Output: 3}
	tests := []struct {
		name    string
		mode    int
		format  relaytypes.RelayFormat
		model   string
		stream  bool
		fixture func(*testing.T) bedrockReplyFixture
		convert func(*Adaptor, *gin.Context, *relaycommon.RelayInfo) (any, error)
		want    metering.Units
	}{
		{name: "chat, writes split by TTL", mode: relayconstant.RelayModeChatCompletions, format: relaytypes.RelayFormatOpenAI, model: "eu.amazon.nova-lite-v1:0",
			fixture: func(*testing.T) bedrockReplyFixture { return reply(byTTL) }, convert: chat, want: split},
		{name: "chat, writes without a TTL are 5-minute writes", mode: relayconstant.RelayModeChatCompletions, format: relaytypes.RelayFormatOpenAI, model: "eu.amazon.nova-lite-v1:0",
			fixture: func(*testing.T) bedrockReplyFixture { return reply(noTTL) }, convert: chat,
			want: metering.Units{Input: 10, CacheRead: 100, CacheWrite: 20, Output: 3}},
		{name: "chat stream", mode: relayconstant.RelayModeChatCompletions, format: relaytypes.RelayFormatOpenAI, model: "eu.amazon.nova-lite-v1:0", stream: true,
			fixture: streamed(byTTL), convert: chat, want: split},
		{name: "Anthropic Messages for a model other than Claude", mode: relayconstant.RelayModeUnknown, format: relaytypes.RelayFormatClaude, model: "qwen.qwen3-32b-v1:0",
			fixture: func(*testing.T) bedrockReplyFixture { return reply(byTTL) }, convert: messages, want: split},
		{name: "Responses", mode: relayconstant.RelayModeResponses, format: relaytypes.RelayFormatOpenAIResponses, model: "eu.anthropic.claude-sonnet-4-6",
			fixture: func(*testing.T) bedrockReplyFixture { return reply(byTTL) }, convert: responses, want: split},
		{name: "Responses stream", mode: relayconstant.RelayModeResponses, format: relaytypes.RelayFormatOpenAIResponses, model: "eu.anthropic.claude-sonnet-4-6", stream: true,
			fixture: streamed(byTTL), convert: responses, want: split},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := startBedrockStub(t, test.fixture(t))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			info := newBedrockTestInfo(server, test.mode, test.format, test.model, test.stream)
			metering.Record(c, metering.Units{Input: 999})

			_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return test.convert(a, c, info) })
			require.Nil(t, apiErr)

			units, ok := metering.Recorded(c)
			require.True(t, ok)
			assert.Equal(t, test.want, units)
		})
	}

	t.Run("an attempt that fails leaves nothing recorded", func(t *testing.T) {
		server, _ := startBedrockStub(t, bedrockReplyFixture{status: 400, header: map[string]string{"X-Amzn-ErrorType": "ValidationException"}, body: []byte(`{"message":"bad"}`)})
		c := newAwsTestContext(httptest.NewRecorder(), context.Background())
		info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, "eu.amazon.nova-lite-v1:0", false)
		metering.Record(c, split)

		_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return chat(a, c, info) })
		require.NotNil(t, apiErr)

		_, ok := metering.Recorded(c)
		assert.False(t, ok)
	})
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
		// Images Bedrock prices per image: Titan Multimodal's, Cohere v3's and Marengo's, not
		// Cohere v4's; texts it prices per request: Marengo's.
		wantImages int64
		wantTexts  int64
	}{
		{
			family: "Titan Text v2: one call per input, dimensions", model: "amazon.titan-embed-text-v2:0", request: `{"input":["a","b"],"dimensions":256}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"embedding":[1,2]}`)}, {header: tokens, body: []byte(`{"embedding":[3,4]}`)}},
			wantPath: "/model/amazon.titan-embed-text-v2%3A0/invoke", wantBody: []string{`{"inputText":"a","dimensions":256}`, `{"inputText":"b","dimensions":256}`},
			wantFirst: []float64{1, 2}, wantCount: 2, wantUsage: 8,
		},
		{
			family: "Titan Text G1: one call per input, its own 1536 dimensions", model: "amazon.titan-embed-text-v1", request: `{"input":["a","b"],"dimensions":1536}`,
			replies:  []bedrockReplyFixture{{body: []byte(`{"embedding":[1,2],"inputTextTokenCount":2}`)}, {body: []byte(`{"embedding":[3,4],"inputTextTokenCount":3}`)}},
			wantPath: "/model/amazon.titan-embed-text-v1/invoke", wantBody: []string{`{"inputText":"a"}`, `{"inputText":"b"}`},
			wantFirst: []float64{1, 2}, wantCount: 2, wantUsage: 5,
		},
		{
			family: "Titan Text G1 under its g1-text-02 id", model: "amazon.titan-embed-g1-text-02", request: `{"input":"a"}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"embedding":[9],"inputTextTokenCount":4}`)}},
			wantPath: "/model/amazon.titan-embed-g1-text-02/invoke", wantBody: []string{`{"inputText":"a"}`},
			wantFirst: []float64{9}, wantCount: 1, wantUsage: 4,
		},
		{
			family: "Titan Multimodal: an image data URL", model: "amazon.titan-embed-image-v1", request: `{"input":"data:image/png;base64,aW1n","dimensions":384}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"embedding":[0.5],"inputTextTokenCount":0}`)}},
			wantPath: "/model/amazon.titan-embed-image-v1/invoke", wantBody: []string{`{"inputImage":"aW1n","embeddingConfig":{"outputEmbeddingLength":384}}`},
			wantFirst: []float64{0.5}, wantCount: 1, wantUsage: 4, wantImages: 1,
		},
		{
			family: "Cohere Embed v3: an image and a text", model: "cohere.embed-english-v3", request: `{"input":["data:image/png;base64,aW1n","a"]}`,
			replies: []bedrockReplyFixture{
				{header: tokens, body: []byte(`{"id":"x","response_type":"embeddings_by_type","embeddings":{"float":[[3,3]]}}`)},
				{header: tokens, body: []byte(`{"id":"y","response_type":"embeddings_by_type","embeddings":{"float":[[1,1]]},"texts":["a"]}`)},
			},
			wantPath:  "/model/cohere.embed-english-v3/invoke",
			wantBody:  []string{`{"images":["data:image/png;base64,aW1n"],"input_type":"image","embedding_types":["float"]}`, `{"texts":["a"],"input_type":"search_document","embedding_types":["float"]}`},
			wantFirst: []float64{3, 3}, wantCount: 2, wantUsage: 8, wantImages: 1,
		},
		{
			family: "Cohere Embed v4: an image is priced by its tokens", model: "eu.cohere.embed-v4:0", request: `{"input":"data:image/png;base64,aW1n"}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"id":"x","response_type":"embeddings_floats","embeddings":[[5,6]]}`)}},
			wantPath: "/model/eu.cohere.embed-v4%3A0/invoke", wantBody: []string{`{"images":["data:image/png;base64,aW1n"],"input_type":"image","embedding_types":["float"]}`},
			wantFirst: []float64{5, 6}, wantCount: 1, wantUsage: 4,
		},
		{
			family: "Nova Multimodal: a text, its tokens from the header", model: "amazon.nova-2-multimodal-embeddings-v1:0", request: `{"input":"a","dimensions":256}`,
			replies:  []bedrockReplyFixture{{header: tokens, body: []byte(`{"embeddings":[{"embeddingType":"TEXT","embedding":[1,2]}]}`)}},
			wantPath: "/model/amazon.nova-2-multimodal-embeddings-v1%3A0/invoke", wantBody: []string{`{"taskType":"SINGLE_EMBEDDING","singleEmbeddingParams":{"embeddingPurpose":"GENERIC_INDEX","embeddingDimension":256,"text":{"truncationMode":"END","value":"a"}}}`},
			wantFirst: []float64{1, 2}, wantCount: 1, wantUsage: 4,
		},
		{
			family: "Nova Multimodal: an image is a standard image, priced per image, with no tokens", model: "amazon.nova-2-multimodal-embeddings-v1:0", request: `{"input":"data:image/jpg;base64,aW1n"}`,
			replies:  []bedrockReplyFixture{{header: map[string]string{bedrockInputTokenHeader: "0"}, body: []byte(`{"embeddings":[{"embeddingType":"IMAGE","embedding":[3]}]}`)}},
			wantPath: "/model/amazon.nova-2-multimodal-embeddings-v1%3A0/invoke", wantBody: []string{`{"taskType":"SINGLE_EMBEDDING","singleEmbeddingParams":{"embeddingPurpose":"GENERIC_INDEX","image":{"format":"jpeg","detailLevel":"STANDARD_IMAGE","source":{"bytes":"aW1n"}}}}`},
			wantFirst: []float64{3}, wantCount: 1, wantUsage: 0, wantImages: 1,
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
		{
			family: "Marengo: a text and an image, one call each, priced per request and image, no tokens", model: "twelvelabs.marengo-embed-3-0-v1:0", request: `{"input":["a dog","data:image/png;base64,aW1n"],"dimensions":512}`,
			replies: []bedrockReplyFixture{
				{body: []byte(`{"data":[{"embedding":[0.25,0.5]}]}`)},
				{body: []byte(`{"data":{"embedding":[0.75]}}`)},
			},
			wantPath:  "/model/twelvelabs.marengo-embed-3-0-v1%3A0/invoke",
			wantBody:  []string{`{"inputType":"text","text":{"inputText":"a dog"}}`, `{"inputType":"image","image":{"mediaSource":{"base64String":"aW1n"}}}`},
			wantFirst: []float64{0.25, 0.5}, wantCount: 2, wantUsage: 0, wantImages: 1, wantTexts: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.family, func(t *testing.T) {
			server, captured := startBedrockStub(t, test.replies...)
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			info := newBedrockTestInfo(server, relayconstant.RelayModeEmbeddings, relaytypes.RelayFormatEmbedding, test.model, false)
			info.SetEstimatePromptTokens(99)
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
			units, ok := metering.Recorded(c)
			require.True(t, ok)
			assert.Equal(t, metering.Units{Input: int64(test.wantUsage), InputImages: test.wantImages, InputTexts: test.wantTexts}, units)
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

	t.Run("Titan Text G1 refuses another vector length", func(t *testing.T) {
		dimensions := 256
		_, err := convertEmbeddingRequest("amazon.titan-embed-text-v1", dto.EmbeddingRequest{Input: "a", Dimensions: &dimensions})
		var apiErr *relaytypes.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	})

	t.Run("Marengo refuses another vector length, and video or audio outside a task", func(t *testing.T) {
		dimensions := 1024
		for _, request := range []dto.EmbeddingRequest{{Input: "a", Dimensions: &dimensions}, {Input: "data:video/mp4;base64,dmlk"}, {Input: "data:audio/wav;base64,YXVk"}} {
			_, err := convertEmbeddingRequest("twelvelabs.marengo-embed-3-0-v1:0", request)
			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		}
	})

	t.Run("a chat model is refused", func(t *testing.T) {
		_, err := convertEmbeddingRequest("eu.amazon.nova-lite-v1:0", dto.EmbeddingRequest{Input: "a"})
		var apiErr *relaytypes.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	})
}

func TestBedrockRerank(t *testing.T) {
	const model = "cohere.rerank-v3-5:0"
	t.Run("a rerank whose units cannot be counted is refused before Bedrock", func(t *testing.T) {
		rerankunits.Install(nil)
		t.Setenv("ESSERA_RERANK_TOKENIZER", filepath.Join(t.TempDir(), "missing.json"))
		c := newAwsTestContext(httptest.NewRecorder(), context.Background())
		_, err := (&Adaptor{}).ConvertRerankRequest(c, relayconstant.RelayModeRerank, dto.RerankRequest{Query: "q", Documents: []any{"a"}})
		var apiErr *relaytypes.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	})
	tiny, err := os.ReadFile("../../../essera/rerankunits/testdata/tiny-tokenizer.json")
	require.NoError(t, err)
	tokenizer, err := rerankunits.Parse(tiny)
	require.NoError(t, err)
	rerankunits.Install(tokenizer)

	t.Run("Cohere Rerank 3.5: best first, documents returned, search units counted", func(t *testing.T) {
		server, captured := startBedrockStub(t, jsonFixture(`{"results":[{"index":2,"relevance_score":0.1},{"index":0,"relevance_score":0.9}]}`))
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		info := newBedrockTestInfo(server, relayconstant.RelayModeRerank, relaytypes.RelayFormatRerank, model, false)
		// With the test tokenizer each "w" is a token: 150 documents of one chunk each.
		documents := make([]any, 150)
		for i := range documents {
			documents[i] = "w"
		}
		documents[0] = map[string]any{"text": "Paris"}
		topN, returnDocuments := 2, true
		request := dto.RerankRequest{Model: model, Query: "capital of France", Documents: documents, TopN: &topN, ReturnDocuments: &returnDocuments}

		_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertRerankRequest(c, info.RelayMode, request) })
		require.Nil(t, apiErr)

		requests := captured()
		require.Len(t, requests, 1)
		assert.Equal(t, "/model/cohere.rerank-v3-5%3A0/invoke", requests[0].path)
		assert.Equal(t, "capital of France", ojson.GetBytes(requests[0].body, "query").String())
		assert.Equal(t, "Paris", ojson.GetBytes(requests[0].body, "documents.0").String())
		assert.EqualValues(t, 150, ojson.GetBytes(requests[0].body, "documents.#").Int())
		assert.EqualValues(t, 2, ojson.GetBytes(requests[0].body, "top_n").Int())
		assert.EqualValues(t, 2, ojson.GetBytes(requests[0].body, "api_version").Int())
		assert.JSONEq(t, `[{"index":0,"relevance_score":0.9,"document":{"text":"Paris"}},{"index":2,"relevance_score":0.1,"document":{"text":"w"}}]`, ojson.GetBytes(recorder.Body.Bytes(), "results").Raw)
		units, ok := metering.Recorded(c)
		require.True(t, ok)
		assert.Equal(t, metering.Units{Generated: 2}, units)
	})

	refused := map[string]dto.RerankRequest{
		"no query":                    {Documents: []any{"a"}},
		"no documents":                {Query: "q"},
		"over 1000 documents":         {Query: "q", Documents: make([]any, 1001)},
		"a document that is not text": {Query: "q", Documents: []any{42}},
		"top_n over the documents":    {Query: "q", Documents: []any{"a"}, TopN: common.GetPointer(2)},
	}
	for name, request := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := (&Adaptor{}).ConvertRerankRequest(nil, relayconstant.RelayModeRerank, request)
			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.True(t, relaytypes.IsSkipRetryError(apiErr))
		})
	}
	t.Run("a long document is sent as it is: Bedrock splits it", func(t *testing.T) {
		c := newAwsTestContext(httptest.NewRecorder(), context.Background())
		_, err := (&Adaptor{}).ConvertRerankRequest(c, relayconstant.RelayModeRerank, dto.RerankRequest{Query: "q", Documents: []any{strings.Repeat("a", 100_000)}})
		require.NoError(t, err)
	})
	t.Run("search units count the chunks of every document with the query", func(t *testing.T) {
		long := strings.TrimSpace(strings.Repeat("w ", 498))
		documents := make([]any, 51)
		for i := range documents {
			documents[i] = long
		}
		c := newAwsTestContext(httptest.NewRecorder(), context.Background())
		adaptor := &Adaptor{}
		_, err := adaptor.ConvertRerankRequest(c, relayconstant.RelayModeRerank, dto.RerankRequest{Query: "w w w", Documents: documents})
		require.NoError(t, err)
		assert.EqualValues(t, 2, adaptor.rerankPlan.searchUnits, "51 documents of 501 tokens with the query are 102 chunks")
	})
}

func TestBedrockStabilityImageEdits(t *testing.T) {
	const inpaint = "stability.stable-image-inpaint-v1:0"
	relayImage := func(t *testing.T, c *gin.Context, model string, mode int, request dto.ImageRequest) (*httptest.Server, func() []capturedBedrockRequest, *relaytypes.NewAPIError) {
		server, captured := startBedrockStub(t, jsonFixture(`{"seeds":[1],"finish_reasons":[null],"images":["aW1hZ2U="]}`))
		info := newBedrockTestInfo(server, mode, relaytypes.RelayFormatOpenAIImage, model, false)
		info.ApiKey = "test-api-key|us-east-1"
		_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertImageRequest(c, info, request) })
		return server, captured, apiErr
	}

	t.Run("multipart: image, mask and the service's own fields, through the us. profile", func(t *testing.T) {
		var form bytes.Buffer
		writer := multipart.NewWriter(&form)
		for name, content := range map[string]string{"image": "img", "mask": "msk"} {
			part, err := writer.CreateFormFile(name, name+".png")
			require.NoError(t, err)
			_, _ = part.Write([]byte(content))
		}
		for name, value := range map[string]string{"model": inpaint, "prompt": "a cat", "seed": "7", "negative_prompt": "dog", "output_format": "jpeg", "ignored": "x"} {
			require.NoError(t, writer.WriteField(name, value))
		}
		require.NoError(t, writer.Close())
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &form)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		require.NoError(t, c.Request.ParseMultipartForm(1<<20))

		_, captured, apiErr := relayImage(t, c, inpaint, relayconstant.RelayModeImagesEdits, dto.ImageRequest{Model: inpaint, Prompt: "a cat"})
		require.Nil(t, apiErr)
		requests := captured()
		require.Len(t, requests, 1)
		assert.Equal(t, "/model/us.stability.stable-image-inpaint-v1%3A0/invoke", requests[0].path)
		assert.JSONEq(t, `{"prompt":"a cat","image":"aW1n","mask":"bXNr","seed":7,"negative_prompt":"dog","output_format":"jpeg"}`, string(requests[0].body))
		assert.Equal(t, "aW1hZ2U=", ojson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String())
		units, ok := metering.Recorded(c)
		require.True(t, ok)
		assert.Equal(t, metering.Units{Generated: 1}, units)
	})

	t.Run("JSON: style transfer takes its two images", func(t *testing.T) {
		const model = "stability.stable-style-transfer-v1:0"
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		var request dto.ImageRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"`+model+`","prompt":"","images":[{"image_url":"data:image/png;base64,aW5pdA=="},{"image_url":"c3R5bGU="}],"style_strength":0.5}`), &request))

		_, captured, apiErr := relayImage(t, c, model, relayconstant.RelayModeImagesEdits, request)
		require.Nil(t, apiErr)
		assert.JSONEq(t, `{"init_image":"aW5pdA==","style_image":"c3R5bGU=","style_strength":0.5}`, string(captured()[0].body))
	})

	refused := map[string]struct {
		model   string
		mode    int
		request string
	}{
		"text to image":               {inpaint, relayconstant.RelayModeImagesGenerations, `{"prompt":"a cat"}`},
		"more than one image":         {inpaint, relayconstant.RelayModeImagesEdits, `{"prompt":"a cat","n":2,"image":"aW1n"}`},
		"a URL answer":                {inpaint, relayconstant.RelayModeImagesEdits, `{"prompt":"a cat","response_format":"url","image":"aW1n"}`},
		"a linked image":              {inpaint, relayconstant.RelayModeImagesEdits, `{"prompt":"a cat","image":"https://example.com/a.png"}`},
		"no image":                    {inpaint, relayconstant.RelayModeImagesEdits, `{"prompt":"a cat"}`},
		"style transfer one":          {"stability.stable-style-transfer-v1:0", relayconstant.RelayModeImagesEdits, `{"prompt":"","image":"aW1n"}`},
		"a field that is no number":   {inpaint, relayconstant.RelayModeImagesEdits, `{"prompt":"a cat","image":"aW1n","seed":"x"}`},
		"a model that makes no image": {"amazon.nova-lite-v1:0", relayconstant.RelayModeImagesEdits, `{"prompt":"a cat","image":"aW1n"}`},
	}
	for name, test := range refused {
		t.Run(name, func(t *testing.T) {
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(test.request), &request))
			_, err := convertStabilityRequest(c, test.mode, test.model, request)
			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		})
	}
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

type fakeAsyncInvoker struct {
	started []*bedrockruntime.StartAsyncInvokeInput
	region  string
}

func (f *fakeAsyncInvoker) StartAsyncInvoke(_ context.Context, input *bedrockruntime.StartAsyncInvokeInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.StartAsyncInvokeOutput, error) {
	f.started = append(f.started, input)
	return &bedrockruntime.StartAsyncInvokeOutput{InvocationArn: aws.String("arn:aws:bedrock:us-east-1:123456789012:async-invoke/inv1")}, nil
}

func (f *fakeAsyncInvoker) GetAsyncInvoke(context.Context, *bedrockruntime.GetAsyncInvokeInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.GetAsyncInvokeOutput, error) {
	return &bedrockruntime.GetAsyncInvokeOutput{
		Status: bedrockruntimeTypes.AsyncInvokeStatusCompleted,
		OutputDataConfig: &bedrockruntimeTypes.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig{
			Value: bedrockruntimeTypes.AsyncInvokeS3OutputDataConfig{S3Uri: aws.String("s3://out-bucket/embedding-tasks/embtask-1/inv1")},
		},
	}, nil
}

func TestMarengoEmbeddingTasks(t *testing.T) {
	const marengo = "twelvelabs.marengo-embed-3-0-v1:0"
	original := EmbeddingTaskStarted
	t.Cleanup(func() { EmbeddingTaskStarted = original })
	var kept []EmbeddingTask
	EmbeddingTaskStarted = func(_ *gin.Context, _ *relaycommon.RelayInfo, task EmbeddingTask) (any, error) {
		kept = append(kept, task)
		return map[string]string{"task_id": task.ID, "status": "SUBMITTED"}, nil
	}

	t.Run("a video's options become Marengo's asynchronous input", func(t *testing.T) {
		start, end := 1.5, 9.0
		plan, err := convertEmbeddingTaskRequest(marengo, dto.EmbeddingRequest{Input: "data:video/mp4;base64,dmlk"}, embeddingTaskRequest{
			StartSec: &start, EndSec: &end, EmbeddingOption: []string{"visual"}, EmbeddingScope: []string{"clip"},
			Segmentation: map[string]any{"method": "fixed", "fixed": map[string]any{"durationSec": 6}},
		})
		require.NoError(t, err)
		assert.Equal(t, "video", plan.Modality)
		body, err := common.Marshal(plan.ModelInput)
		require.NoError(t, err)
		assert.JSONEq(t, `{"inputType":"video","video":{"mediaSource":{"base64String":"dmlk"},"startSec":1.5,"endSec":9,"embeddingOption":["visual"],"embeddingScope":["clip"],"segmentation":{"method":"fixed","fixed":{"durationSec":6}}}}`, string(body))
	})

	t.Run("a task takes one video or audio of Marengo, within its 4 hours", func(t *testing.T) {
		start, end, tooLong := 5.0, 5.0, 4*60*60+1.0
		refused := []struct {
			model   string
			request dto.EmbeddingRequest
			options embeddingTaskRequest
		}{
			{marengo, dto.EmbeddingRequest{Input: "a dog"}, embeddingTaskRequest{}},
			{marengo, dto.EmbeddingRequest{Input: "data:image/png;base64,aW1n"}, embeddingTaskRequest{}},
			{marengo, dto.EmbeddingRequest{Input: []any{"data:audio/wav;base64,YQ==", "data:audio/wav;base64,Yg=="}}, embeddingTaskRequest{}},
			{marengo, dto.EmbeddingRequest{Input: "data:audio/wav;base64,YQ=="}, embeddingTaskRequest{StartSec: &start, EndSec: &end}},
			{marengo, dto.EmbeddingRequest{Input: "data:audio/wav;base64,YQ=="}, embeddingTaskRequest{EndSec: &tooLong}},
			{"amazon.titan-embed-text-v2:0", dto.EmbeddingRequest{Input: "data:video/mp4;base64,dmlk"}, embeddingTaskRequest{}},
		}
		for _, test := range refused {
			_, err := convertEmbeddingTaskRequest(test.model, test.request, test.options)
			var apiErr *relaytypes.NewAPIError
			require.ErrorAs(t, err, &apiErr, "%v", test.request.Input)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		}
	})

	t.Run("the submission starts the invocation on the base model with the task role and keeps the task", func(t *testing.T) {
		t.Setenv(OutputBucketEnv, "out-bucket")
		invoker := &fakeAsyncInvoker{}
		originalInvoker := newAsyncInvoker
		t.Cleanup(func() { newAsyncInvoker = originalInvoker })
		newAsyncInvoker = func(_ context.Context, region string) (asyncInvoker, error) {
			invoker.region = region
			return invoker, nil
		}
		recorder := httptest.NewRecorder()
		c := newAwsTestContext(recorder, context.Background())
		body := `{"model":"us.` + marengo + `","input":"data:audio/wav;base64,YXVk","embedding_option":["audio"]}`
		c.Request = httptest.NewRequest(http.MethodPost, EmbeddingTaskPath, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		unused := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(unused.Close)
		info := newBedrockTestInfo(unused, relayconstant.RelayModeEmbeddings, relaytypes.RelayFormatEmbedding, "us."+marengo, false)
		info.ApiKey = "test-api-key|us-east-1"
		info.RequestURLPath = EmbeddingTaskPath
		var request dto.EmbeddingRequest
		require.NoError(t, common.Unmarshal([]byte(body), &request))

		usage, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertEmbeddingRequest(c, info, request) })
		require.Nil(t, apiErr)
		assert.Zero(t, usage.PromptTokens)
		require.Len(t, invoker.started, 1)
		started := invoker.started[0]
		assert.Equal(t, "us-east-1", invoker.region)
		assert.Equal(t, marengo, aws.ToString(started.ModelId), "Bedrock starts an invocation on the base model only")
		taskID := aws.ToString(started.ClientRequestToken)
		assert.True(t, strings.HasPrefix(taskID, "embtask-"))
		output := started.OutputDataConfig.(*bedrockruntimeTypes.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig)
		assert.Equal(t, "s3://out-bucket/embedding-tasks/"+taskID+"/", aws.ToString(output.Value.S3Uri))
		input, err := started.ModelInput.MarshalSmithyDocument()
		require.NoError(t, err)
		assert.JSONEq(t, `{"inputType":"audio","audio":{"mediaSource":{"base64String":"YXVk"},"embeddingOption":["audio"]}}`, string(input))
		require.Len(t, kept, 1)
		assert.Equal(t, EmbeddingTask{ID: taskID, Model: "us." + marengo, Modality: "audio", InvocationARN: "arn:aws:bedrock:us-east-1:123456789012:async-invoke/inv1", Region: "us-east-1"}, kept[0])
		assert.JSONEq(t, `{"task_id":"`+taskID+`","status":"SUBMITTED"}`, recorder.Body.String())
		_, recorded := metering.Recorded(c)
		assert.False(t, recorded, "a task's units are reported when it ends")
	})

	t.Run("a completed task's output is read from where Bedrock wrote it", func(t *testing.T) {
		originalInvoker, originalReader := newAsyncInvoker, readTaskOutput
		t.Cleanup(func() { newAsyncInvoker, readTaskOutput = originalInvoker, originalReader })
		newAsyncInvoker = func(context.Context, string) (asyncInvoker, error) { return &fakeAsyncInvoker{}, nil }
		var read string
		readTaskOutput = func(_ context.Context, region, bucket, key string) ([]byte, error) {
			read = region + " " + bucket + " " + key
			return []byte(`{"data":[{"embedding":[0.5],"embeddingOption":"visual","embeddingScope":"asset","startSec":0.0,"endSec":4.6},{"embedding":[0.25],"embeddingOption":"visual","embeddingScope":"clip","startSec":0.0,"endSec":4.600000381469727}]}`), nil
		}
		state, err := PollEmbeddingTask(context.Background(), EmbeddingTask{InvocationARN: "arn", Region: "us-east-1"})
		require.NoError(t, err)
		assert.Equal(t, EmbeddingTaskState{Done: true, OutputURI: "s3://out-bucket/embedding-tasks/embtask-1/inv1"}, state)
		items, err := EmbeddingTaskOutput(context.Background(), "us-east-1", state.OutputURI)
		require.NoError(t, err)
		assert.Equal(t, "us-east-1 out-bucket embedding-tasks/embtask-1/inv1/output.json", read)
		require.Len(t, items, 2)
		assert.Equal(t, EmbeddingTaskItem{Object: "embedding", Index: 1, Embedding: []float64{0.25}, EmbeddingOption: "visual", EmbeddingScope: "clip", StartSec: 0, EndSec: 4.600000381469727}, items[1])
		assert.Equal(t, int64(5), MediaSeconds(items))
	})
}

func TestMediaSecondsAreTheSpanRoundedUp(t *testing.T) {
	span := func(start, end float64) EmbeddingTaskItem { return EmbeddingTaskItem{StartSec: start, EndSec: end} }
	tests := []struct {
		name  string
		items []EmbeddingTaskItem
		want  int64
	}{
		{"no embeddings", nil, 0},
		{"a whole second", []EmbeddingTaskItem{span(0, 3)}, 3},
		{"float noise past a whole second is no extra second", []EmbeddingTaskItem{span(0, 3.0000001)}, 3},
		{"a part of a second is a second", []EmbeddingTaskItem{span(0, 3.4)}, 4},
		{"clips from start_sec to end_sec", []EmbeddingTaskItem{span(10, 16), span(16, 21.5)}, 12},
		{"at most 4 hours", []EmbeddingTaskItem{span(0, 1e9)}, 4 * 60 * 60},
		{"an end before the start is nothing", []EmbeddingTaskItem{span(5, 2)}, 0},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, MediaSeconds(test.items), test.name)
	}
}
