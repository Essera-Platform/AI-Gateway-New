// Essera fork: reasoning_effort on Bedrock Claude ids.
package aws

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/stretchr/testify/require"
)

func convertBedrockClaude(t *testing.T, request dto.GeneralOpenAIRequest) *dto.ClaudeRequest {
	t.Helper()
	info := &relaycommon.RelayInfo{
		OriginModelName: request.Model,
		Request:         &request,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: request.Model},
	}
	claudeRequest, err := relayconvert.OpenAIChatRequestToClaudeMessages(nil, info, request)
	require.NoError(t, err)
	return claudeRequest
}

func TestBedrockClaudeReasoningEffort(t *testing.T) {
	messages := []dto.Message{{Role: "user", Content: "hi"}}
	maxTokens := uint(4096)

	for _, model := range []string{"global.anthropic.claude-fable-5-2", "eu.anthropic.claude-opus-5", "anthropic.claude-sonnet-5-5", "anthropic.claude-opus-4-7", "eu.anthropic.claude-haiku-5-5", "anthropic.claude-sonnet-4-6"} {
		request := convertBedrockClaude(t, dto.GeneralOpenAIRequest{Model: model, Messages: messages, MaxTokens: &maxTokens, ReasoningEffort: "medium"})
		require.NotNil(t, request.Thinking, model)
		require.Equal(t, "adaptive", request.Thinking.Type, model)
		require.Nil(t, request.Thinking.BudgetTokens, model)
		require.JSONEq(t, `{"effort":"medium"}`, string(request.OutputConfig), model)
	}

	request := convertBedrockClaude(t, dto.GeneralOpenAIRequest{Model: "anthropic.claude-haiku-4-5-20251001-v1:0", Messages: messages, MaxTokens: &maxTokens, ReasoningEffort: "low"})
	require.Equal(t, "enabled", request.Thinking.Type)
	require.NotNil(t, request.Thinking.BudgetTokens)
	require.Empty(t, request.OutputConfig)

	request = convertBedrockClaude(t, dto.GeneralOpenAIRequest{Model: "anthropic.claude-opus-4-5-20251101-v1:0", Messages: messages, MaxTokens: &maxTokens, ReasoningEffort: "high"})
	require.Equal(t, "enabled", request.Thinking.Type)
	require.JSONEq(t, `{"effort":"high"}`, string(request.OutputConfig))
}

func TestBedrockClaudeSamplingNotInjectedNorDropped(t *testing.T) {
	messages := []dto.Message{{Role: "user", Content: "hi"}}
	request := convertBedrockClaude(t, dto.GeneralOpenAIRequest{Model: "eu.anthropic.claude-opus-5", Messages: messages})
	require.Nil(t, request.Temperature)
	require.Nil(t, request.TopP)
	require.Nil(t, request.Thinking)

	temperature := 0.5
	request = convertBedrockClaude(t, dto.GeneralOpenAIRequest{Model: "eu.anthropic.claude-opus-5", Messages: messages, Temperature: &temperature})
	require.Equal(t, &temperature, request.Temperature)
}
