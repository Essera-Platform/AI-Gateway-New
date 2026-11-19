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
	buyers := rating.Buyers(snapshottest.CustomerID, snapshottest.PartnerID, "")

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

	charges := rating.Price(rates, "openai", "gpt-4o", rating.Buyers(snapshottest.CustomerID, snapshottest.PartnerID, ""), at, rating.Usage{Modality: "text", InputUnits: 1_000_000})

	assert.Equal(t, "2.25", charge(t, charges, "input_tokens").Billable.String(), "10% off the 2.50 list rate")
}

func TestARateNotYetInForceOrEndedIsNotUsed(t *testing.T) {
	rates := snapshottest.Sample().Rates.Rates
	ended := at.Add(-time.Hour)
	later := snapshot.Rate{ID: "sell-future", Kind: "selling", ProviderKey: "openai", ModelKey: "gpt-4o", Unit: "output_tokens", PerQuantity: 1_000_000,
		OrganizationID: rates[3].OrganizationID, Method: "fixed", Amount: rates[3].Amount, Currency: "USD", EffectiveFrom: at.Add(time.Hour)}
	rates[3].EffectiveTo = &ended
	rates = append(rates, later)

	charges := rating.Price(rates, "openai", "gpt-4o", rating.Buyers(snapshottest.CustomerID, "", ""), at, rating.Usage{Modality: "text", OutputUnits: 10})

	output := charge(t, charges, "output_tokens")
	assert.Empty(t, output.SellingRateID)
	assert.Nil(t, output.Billable)
}

func TestImagesAreChargedByTheGeneratedUnit(t *testing.T) {
	amount := decimal.RequireFromString("0.04")
	rates := []snapshot.Rate{{ID: "img", Kind: "selling", ProviderKey: "openai", ModelKey: "gpt-image", Unit: "images", PerQuantity: 1,
		OrganizationID: &[]string{snapshottest.CustomerID}[0], Method: "fixed", Amount: &amount, Currency: "USD", EffectiveFrom: at.Add(-time.Hour)}}

	charges := rating.Price(rates, "openai", "gpt-image", rating.Buyers(snapshottest.CustomerID, "", ""), at, rating.Usage{Modality: "image", InputUnits: 20, GeneratedUnits: 3})

	assert.Equal(t, "0.12", charge(t, charges, "images").Billable.String())
}

func price(percent string) *decimal.Decimal {
	value := decimal.RequireFromString(percent)
	return &value
}

func selling(id, organization, endUser, provider, model, unit, method, percent string) snapshot.Rate {
	rate := snapshot.Rate{ID: id, Kind: "selling", ProviderKey: provider, ModelKey: model, Unit: unit, PerQuantity: 1_000_000,
		Method: method, Percent: price(percent), Currency: "USD", EffectiveFrom: at.Add(-time.Hour)}
	if organization != "" {
		rate.OrganizationID = &organization
	}
	if endUser != "" {
		rate.EndUser = &endUser
	}
	return rate
}

// The same cases as the Console's tests/test_pricing.py: change both together.
func TestThePersonThenTheCustomerThenThePartnerThenTheStandardPrice(t *testing.T) {
	list := "list"
	cost := snapshot.Rate{ID: "cost", Kind: "cost", ProviderKey: "bedrock", ModelKey: "nova", Unit: "input_tokens", PerQuantity: 1_000_000,
		CostType: &list, Method: "fixed", Amount: price("0.06"), Currency: "USD", EffectiveFrom: at.Add(-time.Hour)}
	standard := selling("standard", "", "", "*", "*", "*", "markup", "0")
	standard.PerQuantity = 1
	partner := selling("partner", snapshottest.PartnerID, "", "bedrock", "nova", "input_tokens", "markup", "15")
	customer := selling("customer", snapshottest.CustomerID, "", "bedrock", "nova", "input_tokens", "markup", "10")
	person := selling("person", snapshottest.CustomerID, "dr.chen", "bedrock", "nova", "input_tokens", "markup", "5")
	all := []snapshot.Rate{cost, standard, partner, customer, person}
	usage := rating.Usage{Modality: "text", InputUnits: 1_000_000}
	chosen := func(rates []snapshot.Rate, endUser string) string {
		charges := rating.Price(rates, "bedrock", "nova", rating.Buyers(snapshottest.CustomerID, snapshottest.PartnerID, endUser), at, usage)
		return charge(t, charges, "input_tokens").SellingRateID
	}

	assert.Equal(t, "person", chosen(all, "dr.chen"))
	assert.Equal(t, "customer", chosen(all, "someone else"))
	assert.Equal(t, "partner", chosen([]snapshot.Rate{cost, standard, partner}, ""))
	assert.Equal(t, "standard", chosen([]snapshot.Rate{cost, standard}, ""))

	charges := rating.Price([]snapshot.Rate{cost, standard}, "bedrock", "nova", rating.Buyers(snapshottest.CustomerID, "", ""), at, usage)
	assert.Equal(t, "0.06", charge(t, charges, "input_tokens").Billable.String(), "at cost, exact per unit")
}

func TestAPriceForTheModelWinsOverOneForTheProviderAndOneForEveryProvider(t *testing.T) {
	every := selling("every", snapshottest.CustomerID, "", "*", "*", "*", "markup", "1")
	provider := selling("provider", snapshottest.CustomerID, "", "bedrock", "*", "*", "markup", "2")
	model := selling("model", snapshottest.CustomerID, "", "bedrock", "nova", "input_tokens", "markup", "3")
	chosen := func(rates ...snapshot.Rate) string {
		charges := rating.Price(rates, "bedrock", "nova", rating.Buyers(snapshottest.CustomerID, "", ""), at, rating.Usage{Modality: "text", InputUnits: 10})
		return charge(t, charges, "input_tokens").SellingRateID
	}
	assert.Equal(t, "model", chosen(every, provider, model))
	assert.Equal(t, "provider", chosen(every, provider))
	assert.Equal(t, "every", chosen(every))
}

func TestAdjustIsAPercentOnTheStandardPrice(t *testing.T) {
	list := "list"
	cost := snapshot.Rate{ID: "cost", Kind: "cost", ProviderKey: "bedrock", ModelKey: "nova", Unit: "input_tokens", PerQuantity: 1_000_000,
		CostType: &list, Method: "fixed", Amount: price("0.06"), Currency: "USD", EffectiveFrom: at.Add(-time.Hour)}
	standard := selling("standard", "", "", "bedrock", "nova", "input_tokens", "markup", "20")
	discount := selling("discount", snapshottest.CustomerID, "", "bedrock", "*", "*", "adjust", "-10")
	usage := rating.Usage{Modality: "text", InputUnits: 1_000_000}

	charges := rating.Price([]snapshot.Rate{cost, standard, discount}, "bedrock", "nova", rating.Buyers(snapshottest.CustomerID, "", ""), at, usage)
	assert.Equal(t, "0.0648", charge(t, charges, "input_tokens").Billable.String(), "0.06 x 1.2 x 0.9")

	orphan := rating.Price([]snapshot.Rate{cost, discount}, "bedrock", "nova", rating.Buyers(snapshottest.CustomerID, "", ""), at, usage)
	assert.Nil(t, charge(t, orphan, "input_tokens").Billable, "nothing to apply the percent to")
}

func TestTheStandardPriceOnEveryUnitDoesNotChargeTheRequestCount(t *testing.T) {
	list := "list"
	cost := snapshot.Rate{ID: "cost", Kind: "cost", ProviderKey: "bedrock", ModelKey: "nova", Unit: "input_tokens", PerQuantity: 1_000_000,
		CostType: &list, Method: "fixed", Amount: price("0.069"), Currency: "USD", EffectiveFrom: at.Add(-time.Hour)}
	standard := selling("standard", "", "", "*", "*", "*", "markup", "0")

	charges := rating.Price([]snapshot.Rate{cost, standard}, "bedrock", "nova", rating.Buyers(snapshottest.CustomerID, "", ""), at,
		rating.Usage{Modality: "text", InputUnits: 1_000_000})

	require.Len(t, charges, 1, "Bedrock has no per-request cost, so the request count is not charged")
	assert.Equal(t, "0.069", charge(t, charges, "input_tokens").Billable.String())
}
