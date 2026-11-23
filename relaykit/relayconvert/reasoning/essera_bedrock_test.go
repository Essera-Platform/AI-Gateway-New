package reasoning

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderClaudeBedrockIDs(t *testing.T) {
	adaptive := []string{
		"global.anthropic.claude-fable-5", "global.anthropic.claude-fable-5-1", "global.anthropic.claude-fable-5-2",
		"eu.anthropic.claude-haiku-5-5", "anthropic.claude-opus-4-7", "eu.anthropic.claude-opus-4-8",
		"eu.anthropic.claude-opus-5", "anthropic.claude-opus-5-5", "eu.anthropic.claude-sonnet-5", "anthropic.claude-sonnet-5-5",
		"eu.anthropic.claude-opus-4-6-v1", "anthropic.claude-sonnet-4-6",
	}
	maxTokens := uint(4096)
	for _, model := range adaptive {
		for _, effort := range []Effort{EffortLow, EffortMedium, EffortHigh} {
			rendered, err := RenderClaude(model, Intent{Mode: ModeEnabled, Effort: effort}, &maxTokens, 0)
			require.NoError(t, err, model)
			require.Equal(t, "adaptive", rendered.Thinking.Type, model)
			require.Nil(t, rendered.Thinking.BudgetTokens, model)
			require.Equal(t, effort, rendered.OutputEffort, model)
		}
	}

	manual := map[string]Effort{
		"eu.anthropic.claude-haiku-4-5-20251001-v1:0": "",
		"eu.anthropic.claude-opus-4-5-20251101-v1:0":  EffortHigh,
	}
	for model, wantEffort := range manual {
		rendered, err := RenderClaude(model, Intent{Mode: ModeEnabled, Effort: EffortHigh}, &maxTokens, 0)
		require.NoError(t, err, model)
		require.Equal(t, "enabled", rendered.Thinking.Type, model)
		require.Equal(t, 3276, *rendered.Thinking.BudgetTokens, model)
		require.Equal(t, wantEffort, rendered.OutputEffort, model)
	}
}

func TestAnthropicClaudeName(t *testing.T) {
	require.Equal(t, "claude-opus-5", anthropicClaudeName("eu.anthropic.claude-opus-5"))
	require.Equal(t, "claude-fable-5-1", anthropicClaudeName("global.anthropic.claude-fable-5-1"))
	require.Equal(t, "claude-haiku-4-5-20251001-v1:0", anthropicClaudeName("anthropic.claude-haiku-4-5-20251001-v1:0"))
	require.Equal(t, "claude-opus-4-8", anthropicClaudeName("claude-opus-4-8"))
	require.Equal(t, "amazon.nova-pro-v1:0", anthropicClaudeName("amazon.nova-pro-v1:0"))
	require.True(t, IsKnownClaudeModel("eu.anthropic.claude-sonnet-4-6"))
}
