// Essera fork: Claude Fable goes through the region's geographic inference profile, never global.
package aws

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/metering"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fableModels = []string{"anthropic.claude-fable-5", "anthropic.claude-fable-5-1", "anthropic.claude-fable-5-2"}

func TestFableUsesTheRegionsGeographicProfile(t *testing.T) {
	for _, model := range fableModels {
		assert.Equal(t, "us."+model, resolveBedrockModelID(0, model, "us-east-1"), model)
		assert.Equal(t, "eu."+model, resolveBedrockModelID(0, model, "eu-west-1"), model)
		assert.Equal(t, "us."+model, resolveBedrockModelID(0, "us."+model, "us-east-1"), "a profile id is not prefixed again")
	}
}

func TestNoModelResolvesToTheGlobalProfile(t *testing.T) {
	for _, model := range append([]string{
		"anthropic.claude-opus-5", "anthropic.claude-sonnet-5-5", "anthropic.claude-haiku-5-5",
		"amazon.nova-2-lite-v1:0", "mistral.pixtral-large-2502-v1:0", "amazon.nova-9-test-v1:0",
	}, fableModels...) {
		for _, region := range []string{"us-east-1", "eu-west-1"} {
			assert.False(t, strings.HasPrefix(resolveBedrockModelID(0, model, region), "global."), model+" "+region)
			assert.False(t, strings.HasPrefix(regionalProfileID(model, region), "global."), model+" "+region)
		}
	}
}

func TestFableInvokeModelGoesThroughTheRegionalProfileAndRecordsEveryUnit(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() { common.RelayTimeout = originalRelayTimeout })
	const usage = `{"input_tokens":100,"output_tokens":23,"cache_read_input_tokens":1000,"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}`
	want := metering.Units{Input: 100, CacheRead: 1000, CacheWrite: 20, CacheWrite1h: 30, Output: 23}

	for _, model := range fableModels {
		for _, stream := range []bool{false, true} {
			name := model
			if stream {
				name += " stream"
			}
			t.Run(name, func(t *testing.T) {
				c := newAwsTestContext(httptest.NewRecorder(), context.Background())
				info := newAwsTestRelayInfo()
				info.IsStream = stream
				info.RelayMode = relayconstant.RelayModeChatCompletions
				info.OriginModelName = model
				info.UpstreamModelName = model
				info.ApiKey = "test-api-key|us-east-1"
				adaptor := &Adaptor{}
				converted, err := adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: model, Messages: []dto.Message{{Role: "user", Content: "Hi"}}})
				require.NoError(t, err)
				body, err := common.Marshal(converted)
				require.NoError(t, err)
				_, err = adaptor.DoRequest(c, info, bytes.NewReader(body))
				require.NoError(t, err)

				var path string
				adaptor.AwsClient = newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
					path = request.URL.EscapedPath()
					if !stream {
						reply := `{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":` + usage + `}`
						return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: request}, nil
					}
					var events bytes.Buffer
					for _, event := range []string{
						`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[],"usage":` + usage + `}}`,
						`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
						`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}`,
						`{"type":"message_stop"}`,
					} {
						if err := writeAwsStreamEvent(&events, event); err != nil {
							return nil, err
						}
					}
					return newAwsStreamResponse(request, io.NopCloser(bytes.NewReader(events.Bytes()))), nil
				}))

				_, apiErr := adaptor.DoResponse(c, nil, info)
				require.Nil(t, apiErr)
				if stream {
					assert.Equal(t, "us."+model, aws.ToString(adaptor.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput).ModelId))
				} else {
					assert.Equal(t, "us."+model, aws.ToString(adaptor.AwsReq.(*bedrockruntime.InvokeModelInput).ModelId))
				}
				assert.Contains(t, path, "/model/us."+model+"/")
				units, ok := metering.Recorded(c)
				require.True(t, ok)
				assert.Equal(t, want, units)
				assert.Equal(t, model, info.UpstreamModelName, "usage keeps the catalog model")
			})
		}
	}
}

func TestFableConverseGoesThroughTheRegionalProfileAndRecordsEveryUnit(t *testing.T) {
	originalTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
	const byTTL = `{"inputTokens":10,"outputTokens":3,"totalTokens":163,"cacheReadInputTokens":100,"cacheWriteInputTokens":50,"cacheDetails":[{"inputTokens":30,"ttl":"1h"},{"inputTokens":20,"ttl":"5m"}]}`
	want := metering.Units{Input: 10, CacheRead: 100, CacheWrite: 20, CacheWrite1h: 30, Output: 3}

	for _, model := range fableModels {
		for _, stream := range []bool{false, true} {
			name := model
			fixture := jsonFixture(`{"output":{"message":{"role":"assistant","content":[{"text":"Hello"}]}},"stopReason":"end_turn","usage":` + byTTL + `}`)
			action := "converse"
			if stream {
				name += " stream"
				action = "converse-stream"
				fixture = converseStreamFixture(t,
					[2]string{"messageStart", `{"role":"assistant"}`},
					[2]string{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Hello"}}`},
					[2]string{"messageStop", `{"stopReason":"end_turn"}`},
					[2]string{"metadata", `{"usage":` + byTTL + `}`},
				)
			}
			t.Run(name, func(t *testing.T) {
				server, captured := startBedrockStub(t, fixture)
				c := newAwsTestContext(httptest.NewRecorder(), context.Background())
				info := newBedrockTestInfo(server, relayconstant.RelayModeResponses, relaytypes.RelayFormatOpenAIResponses, model, stream)
				info.ApiKey = "test-api-key|us-east-1"
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"`+model+`","input":"Hi"}`), &request))

				_, apiErr := relayThroughBedrock(t, c, info, func(a *Adaptor) (any, error) { return a.ConvertOpenAIResponsesRequest(c, info, request) })
				require.Nil(t, apiErr)
				require.Len(t, captured(), 1)
				assert.Equal(t, "/model/us."+model+"/"+action, captured()[0].path)
				units, ok := metering.Recorded(c)
				require.True(t, ok)
				assert.Equal(t, want, units)
			})
		}
	}
}
