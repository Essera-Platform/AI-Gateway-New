// Package metering holds what a provider reported it billed for the request being relayed, in
// the units the Console prices, from the provider's own usage. The relay records it once a
// response is read; essera/usage reports it.
package metering

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const contextKey = "essera_metered_units"

// Units of one request. Input is the input neither read from nor written to the prompt cache;
// CacheWrite is written with the 5-minute TTL, CacheWrite1h with the 1-hour TTL; Output includes
// reasoning; InputImages are images embedded, which the provider prices per image. A speech
// model bills its speech apart from its text: SpeechInput and SpeechOutput are speech tokens,
// Input and Output its text tokens.
type Units struct {
	Input        int64
	CacheRead    int64
	CacheWrite   int64
	CacheWrite1h int64
	Output       int64
	InputImages  int64
	SpeechInput  int64
	SpeechOutput int64
}

// FromUsage reads a settled usage. Under Anthropic's semantic the prompt tokens exclude the cache
// reads and writes; under OpenAI's they include them. Responses usage carries its cache counts in
// the input details. A cache write without a TTL breakdown was written with the default 5-minute
// TTL.
func FromUsage(usage *dto.Usage) Units {
	if usage == nil {
		return Units{}
	}
	positive := func(n int) int64 { return int64(max(n, 0)) }
	details := []dto.InputTokenDetails{usage.PromptTokensDetails}
	if usage.InputTokensDetails != nil {
		details = append(details, *usage.InputTokensDetails)
	}
	oneHour := positive(usage.ClaudeCacheCreation1hTokens)
	read, written := int64(0), positive(usage.ClaudeCacheCreation5mTokens)+oneHour
	for _, detail := range details {
		read = max(read, positive(detail.CachedTokens))
		written = max(written, positive(detail.CacheCreationTokensTotal()))
	}
	input := max(positive(usage.PromptTokens), positive(usage.InputTokens))
	if usage.UsageSemantic != dto.BillingUsageSemanticAnthropic {
		input = max(input-read-written, 0)
	}
	return Units{
		Input:        input,
		CacheRead:    read,
		CacheWrite:   written - oneHour,
		CacheWrite1h: oneHour,
		Output:       max(positive(usage.CompletionTokens), positive(usage.OutputTokens)),
	}
}

// Record keeps the units of the attempt that answered.
func Record(c *gin.Context, units Units) {
	c.Set(contextKey, units)
}

// Forget drops what an earlier attempt recorded, before another attempt is sent.
func Forget(c *gin.Context) {
	c.Set(contextKey, nil)
}

func Recorded(c *gin.Context) (Units, bool) {
	value, ok := c.Get(contextKey)
	if !ok {
		return Units{}, false
	}
	units, ok := value.(Units)
	return units, ok
}
