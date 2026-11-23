// Package modelparams lists the request parameters each Bedrock model of the
// catalog accepts through the Gateway, as measured against Bedrock in
// eu-west-1 on 2026-11-23 (the same model ids serve us-east-1) with the model ids the Bedrock adaptor calls.
//
// Every chat model in the table accepts tools (one Converse call each with a
// toolConfig, 2026-11-23).
//
// The table must be refreshed whenever the catalog gains a model: a model
// missing from it is offered max_tokens and stream only.
package modelparams

import "strings"

const (
	Temperature     = "temperature"
	TopP            = "top_p"
	MaxTokens       = "max_tokens"
	Stop            = "stop"
	Stream          = "stream"
	ReasoningEffort = "reasoning_effort"
	Input           = "input"
	Dimensions      = "dimensions"
	EncodingFormat  = "encoding_format"
	Tools           = "tools"
	ToolChoice      = "tool_choice"
)

// The tool_choice values a model may accept; ToolChoiceFunction stands for
// {"type":"function","function":{"name":...}}.
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"
	ToolChoiceFunction = "function"
)

// Params is what one model accepts. SamplingExclusive marks a model that
// takes temperature or top_p, never both in one request.
type Params struct {
	Supported         []string `json:"supported_parameters"`
	SamplingExclusive bool     `json:"sampling_exclusive,omitempty"`
	ToolChoices       []string `json:"tool_choices,omitempty"`
}

type capability uint8

const (
	sampling capability = 1 << iota
	samplingExclusive
	stop
	reasoning
	dimensions
	embedding
	// Bedrock refuses toolChoice any and tool for these models.
	toolChoiceAutoOnly
	// Bedrock refuses toolChoice tool (a named function) for these models.
	noNamedToolChoice
)

// Claude models released from Opus 4.7 on reject temperature, top_p and
// top_k; the Gateway renders their reasoning_effort as adaptive thinking.
// Bedrock drops frequency_penalty, presence_penalty and seed on every path,
// so no model lists them.
var table = map[string]capability{
	"amazon.nova-2-lite-v1:0":  sampling | stop | reasoning,
	"amazon.nova-lite-v1:0":    sampling | stop,
	"amazon.nova-micro-v1:0":   sampling | stop,
	"amazon.nova-pro-v1:0":     sampling | stop,
	"eu.amazon.nova-lite-v1:0": sampling | stop,

	"anthropic.claude-fable-5":                 stop | reasoning,
	"anthropic.claude-fable-5-1":               stop | reasoning | toolChoiceAutoOnly,
	"anthropic.claude-fable-5-2":               stop | reasoning | toolChoiceAutoOnly,
	"anthropic.claude-haiku-5-5":               stop | reasoning,
	"anthropic.claude-opus-4-7":                stop | reasoning,
	"anthropic.claude-opus-4-8":                stop | reasoning,
	"anthropic.claude-opus-5":                  stop | reasoning,
	"anthropic.claude-opus-5-5":                stop | reasoning | toolChoiceAutoOnly,
	"anthropic.claude-sonnet-5":                stop | reasoning,
	"anthropic.claude-sonnet-5-5":              stop | reasoning | toolChoiceAutoOnly,
	"anthropic.claude-haiku-4-5-20251001-v1:0": sampling | samplingExclusive | stop | reasoning,
	"anthropic.claude-opus-4-5-20251101-v1:0":  sampling | samplingExclusive | stop | reasoning,
	"anthropic.claude-opus-4-6-v1":             sampling | samplingExclusive | stop | reasoning,
	"anthropic.claude-sonnet-4-6":              sampling | samplingExclusive | stop | reasoning,

	"mistral.mistral-large-2402-v1:0": sampling | stop | noNamedToolChoice,
	"mistral.pixtral-large-2502-v1:0": sampling | stop | noNamedToolChoice,

	// Bedrock refuses stopSequences for these models.
	"google.gemma-3-12b-it":            sampling,
	"google.gemma-3-27b-it":            sampling,
	"google.gemma-3-4b-it":             sampling,
	"minimax.minimax-m2":               sampling,
	"minimax.minimax-m2.1":             sampling,
	"minimax.minimax-m2.5":             sampling,
	"mistral.devstral-2-123b":          sampling,
	"mistral.magistral-small-2509":     sampling,
	"mistral.ministral-3-14b-instruct": sampling,
	"mistral.ministral-3-3b-instruct":  sampling,
	"mistral.ministral-3-8b-instruct":  sampling,
	"mistral.voxtral-mini-3b-2507":     sampling,
	"mistral.voxtral-small-24b-2507":   sampling,
	"nvidia.nemotron-nano-12b-v2":      sampling,
	"nvidia.nemotron-nano-3-30b":       sampling,
	"nvidia.nemotron-nano-9b-v2":       sampling,
	"nvidia.nemotron-super-3-120b":     sampling,
	"openai.gpt-oss-120b-1:0":          sampling | reasoning,
	"openai.gpt-oss-20b-1:0":           sampling | reasoning,
	"openai.gpt-oss-safeguard-120b":    sampling | reasoning,
	"openai.gpt-oss-safeguard-20b":     sampling | reasoning,
	"qwen.qwen3-32b-v1:0":              sampling,
	"qwen.qwen3-coder-30b-a3b-v1:0":    sampling,
	"qwen.qwen3-next-80b-a3b":          sampling,
	"qwen.qwen3-vl-235b-a22b":          sampling,
	"zai.glm-4.7-flash":                sampling,

	"amazon.titan-embed-image-v1":  embedding | dimensions,
	"amazon.titan-embed-text-v2:0": embedding | dimensions,
	"cohere.embed-v4:0":            embedding | dimensions,
	"cohere.embed-english-v3":      embedding,
	"cohere.embed-multilingual-v3": embedding,
}

// Of returns the parameters model accepts; modality is the catalog's and
// decides the set of a model missing from the table.
func Of(model, modality string) Params {
	capabilities, known := table[model]
	if !known && strings.EqualFold(modality, "embedding") {
		capabilities = embedding
	}
	if capabilities&embedding != 0 {
		supported := []string{Input}
		if capabilities&dimensions != 0 {
			supported = append(supported, Dimensions)
		}
		return Params{Supported: append(supported, EncodingFormat)}
	}
	var supported []string
	if capabilities&sampling != 0 {
		supported = append(supported, Temperature, TopP)
	}
	supported = append(supported, MaxTokens)
	if capabilities&stop != 0 {
		supported = append(supported, Stop)
	}
	supported = append(supported, Stream)
	if capabilities&reasoning != 0 {
		supported = append(supported, ReasoningEffort)
	}
	params := Params{Supported: supported, SamplingExclusive: capabilities&samplingExclusive != 0}
	if known {
		params.Supported = append(params.Supported, Tools, ToolChoice)
		switch {
		case capabilities&toolChoiceAutoOnly != 0:
			params.ToolChoices = []string{ToolChoiceAuto, ToolChoiceNone}
		case capabilities&noNamedToolChoice != 0:
			params.ToolChoices = []string{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired}
		default:
			params.ToolChoices = []string{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceFunction}
		}
	}
	return params
}
