package probes

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAProbeAsksForOneTokenOfAOneWordPrompt(t *testing.T) {
	ctx := context.WithValue(t.Context(), contextKey{}, Request{Provider: "bedrock", Model: "nova", Modality: "text"})
	sixteen := uint(16)
	chat := Shape(ctx, &dto.GeneralOpenAIRequest{Model: "nova", MaxTokens: &sixteen, Messages: []dto.Message{{Role: "user", Content: "hi"}}}).(*dto.GeneralOpenAIRequest)

	require.NotNil(t, chat.MaxTokens)
	assert.Equal(t, uint(1), *chat.MaxTokens)
	assert.Equal(t, []dto.Message{{Role: "user", Content: "Hi"}}, chat.Messages)
	require.NotNil(t, chat.Stream)
	assert.True(t, *chat.Stream, "streamed, so the first token is timed")
	require.NotNil(t, chat.StreamOptions)
	assert.True(t, chat.StreamOptions.IncludeUsage)

	embedding := Shape(ctx, &dto.EmbeddingRequest{Model: "titan", Input: []any{"hello world"}}).(*dto.EmbeddingRequest)
	assert.Equal(t, []any{"ok"}, embedding.Input)

	untouched := Shape(t.Context(), &dto.GeneralOpenAIRequest{Model: "nova", MaxTokens: &sixteen}).(*dto.GeneralOpenAIRequest)
	assert.Equal(t, uint(16), *untouched.MaxTokens, "a connection test is not a probe")
}

func TestAProbeRunsOnTheNamedProvidersChannelWithinItsTimeLimit(t *testing.T) {
	recordstest.UseDatabase(t, &model.Channel{})
	tag := "essera/bedrock"
	require.NoError(t, model.DB.Create(&model.Channel{Name: "bedrock", Key: "k", Tag: &tag}).Error)
	var seen *model.Channel
	var deadline time.Time
	RunChannel = func(ctx context.Context, channel *model.Channel, request Request) Result {
		seen, deadline = channel, time.Time{}
		deadline, _ = ctx.Deadline()
		assert.True(t, IsProbe(ctx))
		return Result{Succeeded: true, HTTPStatus: 200, LatencyMs: 300, OutputTokens: 1}
	}
	t.Cleanup(func() { RunChannel = nil })

	result := Run(t.Context(), Request{Provider: "bedrock", Model: "nova", Modality: "text"})

	assert.True(t, result.Succeeded)
	require.NotNil(t, seen)
	assert.Equal(t, "bedrock", seen.Name)
	assert.WithinDuration(t, time.Now().Add(Timeout), deadline, time.Second)

	missing := Run(t.Context(), Request{Provider: "acme", Model: "x", Modality: "text"})
	assert.False(t, missing.Succeeded)
	require.NotNil(t, missing.ErrorClass)
	assert.Equal(t, "essera_no_provider", *missing.ErrorClass)
}

func TestARequestIsValidOnlyWithEveryNameAndAProbedModality(t *testing.T) {
	assert.True(t, Request{Provider: "p", Model: "m", Modality: "text"}.Valid())
	assert.True(t, Request{Provider: "p", Model: "m", Modality: "embedding"}.Valid())
	for _, broken := range []Request{
		{Model: "m", Modality: "text"},
		{Provider: "p", Modality: "text"},
		{Provider: "p", Model: "m", Modality: "image"},
	} {
		assert.False(t, broken.Valid(), broken)
	}
}
