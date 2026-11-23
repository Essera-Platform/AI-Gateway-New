package modelparams

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var allToolChoices = []string{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceFunction}

func TestOf(t *testing.T) {
	tests := []struct {
		name, model, modality string
		want                  Params
	}{
		{"Claude that rejects sampling and forced tool choice", "anthropic.claude-opus-5-5", "text", Params{Supported: []string{MaxTokens, Stop, Stream, ReasoningEffort, Tools, ToolChoice}, ToolChoices: []string{ToolChoiceAuto, ToolChoiceNone}}},
		{"Claude that takes one sampling control", "anthropic.claude-sonnet-4-6", "text", Params{Supported: []string{Temperature, TopP, MaxTokens, Stop, Stream, ReasoningEffort, Tools, ToolChoice}, SamplingExclusive: true, ToolChoices: allToolChoices}},
		{"Nova 2 with reasoning", "amazon.nova-2-lite-v1:0", "text", Params{Supported: []string{Temperature, TopP, MaxTokens, Stop, Stream, ReasoningEffort, Tools, ToolChoice}, ToolChoices: allToolChoices}},
		{"model refusing stop sequences", "qwen.qwen3-32b-v1:0", "text", Params{Supported: []string{Temperature, TopP, MaxTokens, Stream, Tools, ToolChoice}, ToolChoices: allToolChoices}},
		{"model refusing a named tool choice", "mistral.mistral-large-2402-v1:0", "text", Params{Supported: []string{Temperature, TopP, MaxTokens, Stop, Stream, Tools, ToolChoice}, ToolChoices: []string{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired}}},
		{"embedding with dimensions", "cohere.embed-v4:0", "embedding", Params{Supported: []string{Input, Dimensions, EncodingFormat}}},
		{"embedding without dimensions", "cohere.embed-english-v3", "embedding", Params{Supported: []string{Input, EncodingFormat}}},
		{"unmeasured text model", "vendor.new-model", "text", Params{Supported: []string{MaxTokens, Stream}}},
		{"unmeasured embedding model", "vendor.new-embed", "embedding", Params{Supported: []string{Input, EncodingFormat}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, Of(test.model, test.modality))
		})
	}
}

func TestTheUsEast1ModelsListWhatBedrockAccepts(t *testing.T) {
	gpt := Of("openai.gpt-5.6-sol", "text")
	assert.Equal(t, []string{MaxTokens, Stream, ReasoningEffort, Tools, ToolChoice}, gpt.Supported)
	assert.Equal(t, []string{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceFunction}, gpt.ToolChoices)

	assert.Equal(t, []string{MaxTokens, Stream, Tools, ToolChoice}, Of("xai.grok-4.7", "text").Supported)
	assert.Equal(t, []string{Temperature, TopP, MaxTokens, Stream, Tools, ToolChoice}, Of("deepseek.v3.2", "text").Supported)

	palmyra := Of("writer.palmyra-vision-7b", "text")
	assert.Equal(t, []string{Temperature, TopP, MaxTokens, Stream}, palmyra.Supported)
	assert.Empty(t, palmyra.ToolChoices)

	assert.Equal(t, []string{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired}, Of("mistral.mistral-small-2402-v1:0", "text").ToolChoices)
	assert.Equal(t, []string{Input, EncodingFormat}, Of("amazon.titan-embed-text-v1", "embedding").Supported)
}
