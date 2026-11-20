package policy

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

const claude = "anthropic.claude-sonnet-5"

func cachingRequest(rates ...snapshot.Rate) *Request {
	sample := snapshottest.Sample()
	sample.Rates = &snapshot.RatesCatalog{Rates: rates}
	view := snapshot.NewView(7, sample)
	return &Request{Caller: &auth.Caller{View: view, Customer: view.Customer(snapshottest.CustomerID)}}
}

func cost(model, unit string) snapshot.Rate {
	amount := decimal.RequireFromString("0.22")
	return snapshot.Rate{Kind: "cost", ProviderKey: "bedrock", ModelKey: model, Unit: unit, PerQuantity: 1_000_000,
		Method: "fixed", Amount: &amount, Currency: "USD", EffectiveFrom: time.Unix(0, 0)}
}

// The standard price at cost: a 0% markup on every unit of every model.
func standardAtCost() snapshot.Rate {
	zero := decimal.Zero
	return snapshot.Rate{Kind: "selling", ProviderKey: "*", ModelKey: "*", Unit: "*", Method: "markup",
		Percent: &zero, Currency: "USD", EffectiveFrom: time.Unix(0, 0)}
}

func TestPromptCachingFollowsTheRatesOfTheCacheUnits(t *testing.T) {
	now := time.Now()
	read, write, write1h := cost(claude, "cache_read_tokens"), cost(claude, "cache_write_tokens"), cost(claude, "cache_write_1h_tokens")
	ended := cost(claude, "cache_read_tokens")
	past := now.Add(-time.Hour)
	ended.EffectiveTo = &past
	onlyInput := standardAtCost()
	onlyInput.Unit = "input_tokens"

	tests := []struct {
		name  string
		rates []snapshot.Rate
		want  Caching
	}{
		{"reads, 5-minute and 1-hour writes priced", []snapshot.Rate{read, write, write1h, standardAtCost()}, CachingAll},
		{"no 1-hour write cost", []snapshot.Rate{read, write, standardAtCost()}, CachingFiveMinutes},
		{"no read cost", []snapshot.Rate{write, write1h, standardAtCost()}, CachingOff},
		{"an ended read cost", []snapshot.Rate{ended, write, write1h, standardAtCost()}, CachingOff},
		{"no selling price covering the cache units", []snapshot.Rate{read, write, write1h, onlyInput}, CachingOff},
		{"costs of another model", []snapshot.Rate{cost("other", "cache_read_tokens"), cost("other", "cache_write_tokens"), standardAtCost()}, CachingOff},
		{"long-context costs without long-context cache costs", []snapshot.Rate{read, write, write1h, cost(claude, "input_tokens_long"), standardAtCost()}, CachingOff},
		{"long-context cache costs too", []snapshot.Rate{read, write, cost(claude, "input_tokens_long"), cost(claude, "cache_read_tokens_long"), cost(claude, "cache_write_tokens_long"), standardAtCost()}, CachingFiveMinutes},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, cachingRequest(test.rates...).PromptCaching("bedrock", claude, now))
		})
	}
}

func TestPromptCachingTakesTheCustomersOwnSellingPrice(t *testing.T) {
	now := time.Now()
	customer := snapshottest.CustomerID
	own := standardAtCost()
	own.OrganizationID = &customer
	request := cachingRequest(cost(claude, "cache_read_tokens"), cost(claude, "cache_write_tokens"), own)
	assert.Equal(t, CachingFiveMinutes, request.PromptCaching("bedrock", claude, now))
	assert.Equal(t, CachingOff, (*Request)(nil).PromptCaching("bedrock", claude, now))
}
