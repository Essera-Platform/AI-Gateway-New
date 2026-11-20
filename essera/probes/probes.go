// Package probes runs the Console's model probes: one minimal call to a model, sent straight to
// the provider through the channel's adaptor with the Gateway's configured key, as the
// connection test is. It is no customer's request: no caller, policy, limits or usage event,
// and the Gateway's traffic counts leave it out. The Console asks for each probe on the
// internal API (bridge, POST /internal/essera/v1/probes), keeps the result in its own table
// and prices it at the model's cost rate. It costs a one-word prompt and a one-token answer.
package probes

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Timeout bounds one probe, the provider's answer included: within the 15 seconds ECS Service
// Connect gives a request, so the Console hears how the probe ended rather than a cut-off.
const Timeout = 12 * time.Second

const (
	prompt         = "Hi"
	maxTokens      = 1
	embeddingInput = "ok"
)

// Request is the Console's ask: which model of which provider.
type Request struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Modality string `json:"modality"`
}

// Valid tells whether the request names everything and a modality a probe can call.
func (r Request) Valid() bool {
	return r.Provider != "" && r.Model != "" && (r.Modality == "text" || r.Modality == "embedding")
}

// Result is how the probe went, as the Console records it.
type Result struct {
	Succeeded         bool    `json:"succeeded"`
	HTTPStatus        int     `json:"http_status"`
	ErrorClass        *string `json:"error_class"`
	Message           *string `json:"message"`
	LatencyMs         int64   `json:"latency_ms"`
	FirstTokenMs      *int64  `json:"first_token_ms"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	ProviderRequestID *string `json:"provider_request_id"`
}

// RunChannel calls the model through the channel with the probe's request; the controller
// package sets it, since new-api's channel test lives there.
var RunChannel func(ctx context.Context, channel *model.Channel, request Request) Result

// Run probes the model of the provider the Console names.
func Run(ctx context.Context, request Request) Result {
	var channel model.Channel
	if err := model.DB.Where("tag = ?", configsync.TagPrefix+request.Provider).First(&channel).Error; err != nil {
		return Failed(0, "essera_no_provider", "The Gateway has no such provider; its configuration may not have arrived yet.")
	}
	if RunChannel == nil {
		return Failed(0, "essera_probe_unavailable", "The Gateway cannot run probes.")
	}
	ctx, cancel := context.WithTimeout(Context(ctx, request), Timeout)
	defer cancel()
	return RunChannel(ctx, &channel, request)
}

// Failed is a probe that did not get an answer.
func Failed(status int, class, message string) Result {
	return Result{HTTPStatus: status, ErrorClass: &class, Message: &message}
}

type contextKey struct{}

// Context marks a channel test as the probe of request. Only code in this process can set it.
func Context(ctx context.Context, request Request) context.Context {
	return context.WithValue(ctx, contextKey{}, request)
}

// Shape makes the channel test's request the probe's: a one-word prompt with a one-token
// answer, streamed with usage, or a one-word embedding. Any other request is left as it is.
func Shape(ctx context.Context, request dto.Request) dto.Request {
	if _, ok := ctx.Value(contextKey{}).(Request); !ok {
		return request
	}
	switch shaped := request.(type) {
	case *dto.GeneralOpenAIRequest:
		tokens := uint(maxTokens)
		shaped.MaxTokens = &tokens
		shaped.Messages = []dto.Message{{Role: "user", Content: prompt}}
		stream := true
		shaped.Stream = &stream
		shaped.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	case *dto.EmbeddingRequest:
		shaped.Input = []any{embeddingInput}
	}
	return request
}

// IsProbe tells whether the context is a probe's.
func IsProbe(ctx context.Context) bool {
	_, ok := ctx.Value(contextKey{}).(Request)
	return ok
}
