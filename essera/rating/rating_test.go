package rating_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/rating"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var at = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func charge(t *testing.T, charges []rating.Charge, unit string) rating.Charge {
	t.Helper()
	for _, c := range charges {
		if c.Unit == unit {
			return c
		}
	}
	require.Failf(t, "no charge", "unit %s", unit)
	return rating.Charge{}
}

func TestPriceFollowsTheConsolesRules(t *testing.T) {
	rates := snapshottest.Sample().Rates.Rates
	buyers := []string{snapshottest.CustomerID, snapshottest.PartnerID}

	charges := rating.Price(rates, "openai", "gpt-4o", buyers, at, rating.Usage{Modality: "text", InputUnits: 1_000_000, OutputUnits: 500_000})

	input := charge(t, charges, "input_tokens")
	assert.Equal(t, "cost-negotiated", input.CostRateID, "negotiated before list")
	assert.Equal(t, "sell-partner", input.SellingRateID, "the partner's rate when the customer has none")
	assert.Equal(t, "2.4", input.Billable.String(), "20% markup over the 2.00 cost in force")
	output := charge(t, charges, "output_tokens")
	assert.Equal(t, "sell-customer", output.SellingRateID)
	assert.Equal(t, "6", output.Billable.String())
	assert.Len(t, charges, 2, "no per-request rate, so no request charge")
	assert.Equal(t, "8.4", rating.Billable(charges, "USD").String())
}

func TestDiscountIsOffTheListRate(t *testing.T) {
	rates := snapshottest.Sample().Rates.Rates
	percent := decimal.RequireFromString("10")
	for i := range rates {
		if rates[i].ID == "sell-partner" {
			rates[i].Method, rates[i].Percent = "discount", &percent
		}
	}

	charges := rating.Price(rates, "openai", "gpt-4o", []string{snapshottest.CustomerID, snapshottest.PartnerID}, at, rating.Usage{Modality: "text", InputUnits: 1_000_000})

	assert.Equal(t, "2.25", charge(t, charges, "input_tokens").Billable.String(), "10% off the 2.50 list rate")
}

func TestARateNotYetInForceOrEndedIsNotUsed(t *testing.T) {
	rates := snapshottest.Sample().Rates.Rates
	ended := at.Add(-time.Hour)
	later := snapshot.Rate{ID: "sell-future", Kind: "selling", ProviderKey: "openai", ModelKey: "gpt-4o", Unit: "output_tokens", PerQuantity: 1_000_000,
		OrganizationID: rates[3].OrganizationID, Method: "fixed", Amount: rates[3].Amount, Currency: "USD", EffectiveFrom: at.Add(time.Hour)}
	rates[3].EffectiveTo = &ended
	rates = append(rates, later)

	charges := rating.Price(rates, "openai", "gpt-4o", []string{snapshottest.CustomerID}, at, rating.Usage{Modality: "text", OutputUnits: 10})

	output := charge(t, charges, "output_tokens")
	assert.Empty(t, output.SellingRateID)
	assert.Nil(t, output.Billable)
}

func TestImagesAreChargedByTheGeneratedUnit(t *testing.T) {
	amount := decimal.RequireFromString("0.04")
	rates := []snapshot.Rate{{ID: "img", Kind: "selling", ProviderKey: "openai", ModelKey: "gpt-image", Unit: "images", PerQuantity: 1,
		OrganizationID: &[]string{snapshottest.CustomerID}[0], Method: "fixed", Amount: &amount, Currency: "USD", EffectiveFrom: at.Add(-time.Hour)}}

	charges := rating.Price(rates, "openai", "gpt-image", []string{snapshottest.CustomerID}, at, rating.Usage{Modality: "image", InputUnits: 20, GeneratedUnits: 3})

	assert.Equal(t, "0.12", charge(t, charges, "images").Billable.String())
}
