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
		{"rerank", "cohere.rerank-v3-5:0", "rerank", Params{Supported: []string{Query, Documents, TopN, ReturnDocuments}}},
		{"image edit", "stability.stable-image-inpaint-v1:0", "image", Params{Supported: []string{Prompt, Image, Mask}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, Of(test.model, test.modality))
		})
	}
}
