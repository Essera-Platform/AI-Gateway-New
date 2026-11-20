package aws

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/policy"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cachedMessagesBody = `{"model":"claude-opus-4-7","max_tokens":128,"cache_control":{"type":"ephemeral"},"prompt_cache_key":"k","system":[{"type":"text","text":"Long manual","cache_control":{"type":"ephemeral","ttl":"1h"}}],"tools":[{"name":"lookup","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"lookup","input":{"n":12345678901234567890}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"42","cache_control":{"type":"ephemeral"}}]}]},{"role":"user","content":"plain"}]}`

func cachingAllowed(t *testing.T, caching policy.Caching) {
	original := cachingFor
	cachingFor = func(*gin.Context, *relaycommon.RelayInfo) policy.Caching { return caching }
	t.Cleanup(func() { cachingFor = original })
}

func cachingOff(t *testing.T) { cachingAllowed(t, policy.CachingOff) }

func assertNoCaching(t *testing.T, payload []byte) {
	t.Helper()
	text := string(payload)
	assert.NotContains(t, text, "cache_control")
	assert.NotContains(t, text, "prompt_cache")
	assert.NotContains(t, text, "cachePoint")
}

func TestWithoutPromptCachingKeepsEverythingElse(t *testing.T) {
	stripped, err := withoutPromptCaching(strings.NewReader(cachedMessagesBody))
	require.NoError(t, err)
	payload, err := io.ReadAll(stripped)
	require.NoError(t, err)
	assertNoCaching(t, payload)
	assert.JSONEq(t, `{"model":"claude-opus-4-7","max_tokens":128,"system":[{"type":"text","text":"Long manual"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"lookup","input":{"n":12345678901234567890}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"42"}]}]},{"role":"user","content":"plain"}]}`, string(payload))
	assert.Contains(t, string(payload), "12345678901234567890")
}

func TestClaudeInvokeModelSendsNoCachingWhenManaged(t *testing.T) {
	cachingOff(t)
	info := newMantleTestInfo()
	info.ChannelOtherSettings.AwsApiType = dto.AwsApiTypeInvokeModel
	info.IsStream = false
	info.UpstreamModelName = "claude-3-5-sonnet-20240620"
	c := newAwsTestContext(httptest.NewRecorder(), context.Background())
	a := &Adaptor{}
	_, err := a.DoRequest(c, info, strings.NewReader(cachedMessagesBody))
	require.NoError(t, err)
	input, ok := a.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)
	assertNoCaching(t, input.Body)
	assert.Contains(t, string(input.Body), "Long manual")
}

func TestMantleSendsNoCachingWhenManaged(t *testing.T) {
	cachingOff(t)
	var sent []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	original := http.DefaultClient
	http.DefaultClient = server.Client()
	defer func() { http.DefaultClient = original }()
	info := newMantleTestInfo()
	info.ChannelBaseUrl = server.URL
	c := newAwsTestContext(httptest.NewRecorder(), context.Background())
	response, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(cachedMessagesBody))
	require.NoError(t, err)
	response.(*http.Response).Body.Close()
	require.NotEmpty(t, sent)
	assertNoCaching(t, sent)
}

func TestConverseSendsNoCachePointWhenManaged(t *testing.T) {
	cachingOff(t)
	tests := []struct {
		name    string
		format  relaytypes.RelayFormat
		mode    int
		convert func(*Adaptor, *gin.Context, *relaycommon.RelayInfo) (any, error)
	}{
		{
			name: "chat completions", format: relaytypes.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","prompt_cache_key":"k","messages":[{"role":"system","content":[{"type":"text","text":"Long manual","cache_control":{"type":"ephemeral"}}]},{"role":"user","content":[{"type":"text","text":"Q","cache_control":{"type":"ephemeral"}}]}]}`), &request))
				return a.ConvertOpenAIRequest(c, info, &request)
			},
		},
		{
			name: "Anthropic Messages", format: relaytypes.RelayFormatClaude, mode: relayconstant.RelayModeUnknown,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.ClaudeRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","max_tokens":50,"system":[{"type":"text","text":"Long manual","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"Q","cache_control":{"type":"ephemeral"}}]}]}`), &request))
				return a.ConvertClaudeRequest(c, info, &request)
			},
		},
		{
			name: "Responses", format: relaytypes.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses,
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","prompt_cache_key":"k","input":[{"role":"user","content":[{"type":"input_text","text":"Q","cache_control":{"type":"ephemeral"}}]}]}`), &request))
				return a.ConvertOpenAIResponsesRequest(c, info, request)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := startBedrockStub(t, jsonFixture(converseTextReply))
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			info := newBedrockTestInfo(server, test.mode, test.format, "eu.amazon.nova-2-lite-v1:0", false)
			converted, err := test.convert(&Adaptor{}, c, info)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			assertNoCaching(t, body)
			var decoded map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(body, &decoded))
			assert.Contains(t, string(decoded["messages"]), `"Q"`)
		})
	}
}

func invokeBody(t *testing.T, body string) []byte {
	t.Helper()
	info := newMantleTestInfo()
	info.ChannelOtherSettings.AwsApiType = dto.AwsApiTypeInvokeModel
	info.IsStream = false
	info.UpstreamModelName = "claude-3-5-sonnet-20240620"
	c := newAwsTestContext(httptest.NewRecorder(), context.Background())
	a := &Adaptor{}
	_, err := a.DoRequest(c, info, strings.NewReader(body))
	require.NoError(t, err)
	input, ok := a.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)
	return input.Body
}

func TestClaudeKeepsCachingWhereItsUnitsArePriced(t *testing.T) {
	cachingAllowed(t, policy.CachingAll)
	body := invokeBody(t, cachedMessagesBody)
	assert.Contains(t, string(body), `"ttl":"1h"`)
	assert.Equal(t, 4, strings.Count(string(body), "cache_control"), "every block marker; InvokeModel drops only the top-level one")
}

func TestClaudeWritesFiveMinutesWhereOnlyThoseArePriced(t *testing.T) {
	cachingAllowed(t, policy.CachingFiveMinutes)
	body := invokeBody(t, cachedMessagesBody)
	assert.NotContains(t, string(body), `"ttl"`)
	assert.Contains(t, string(body), `"cache_control":{"type":"ephemeral"}`)
	assert.NotContains(t, string(body), "prompt_cache")
}

func TestConverseKeepsTheCachePointWhereCachingIsPriced(t *testing.T) {
	cachingAllowed(t, policy.CachingFiveMinutes)
	server, _ := startBedrockStub(t, jsonFixture(converseTextReply))
	c := newAwsTestContext(httptest.NewRecorder(), context.Background())
	info := newBedrockTestInfo(server, relayconstant.RelayModeChatCompletions, relaytypes.RelayFormatOpenAI, "eu.amazon.nova-2-lite-v1:0", false)
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"eu.amazon.nova-2-lite-v1:0","messages":[{"role":"system","content":[{"type":"text","text":"Long manual","cache_control":{"type":"ephemeral"}}]},{"role":"user","content":"Q"}]}`), &request))
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"cachePoint":{"type":"default"}`)
}
