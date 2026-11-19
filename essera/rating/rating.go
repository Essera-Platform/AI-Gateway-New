// Package rating prices a request with the rates in the configuration, as the Console does
// (apps/backend/app/modules/rates/pricing.py and usage/rating.py in the Console repository;
// change both together). The Console's ledger stays the record of what a request cost; the
// Gateway prices only to enforce spend limits and to name the rate versions it applied on the
// usage event.
//
// The selling price is the first of: one person's price inside the customer, the customer's,
// its partner's, the standard price everyone pays. Within each, a price for the model wins over
// one for the whole provider, which wins over one for every provider; a unit named wins over
// "*"; then the latest start. A cost prefers the negotiated price over the list price.
package rating

import (
	"slices"
	"time"

	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/shopspring/decimal"
)

// Any is the provider, model or unit of a rate that covers them all.
const Any = "*"

// GeneratedUnit is the unit of what a modality generates.
var GeneratedUnit = map[string]string{"image": "images", "video": "video_seconds", "audio": "audio_minutes"}

// Usage is what one request measured.
type Usage struct {
	Modality       string
	InputUnits     int64
	OutputUnits    int64
	GeneratedUnits int64
}

// Charge is one priced unit.
type Charge struct {
	Unit             string
	Quantity         int64
	CostRateID       string
	SellingRateID    string
	Billable         *decimal.Decimal
	BillableCurrency string
}

// Buyer is whose price applies: an organization ("" for the standard price) and a person in it.
type Buyer struct {
	Organization string
	EndUser      string
}

// Buyers are whose prices apply to a customer's request, the first that has one winning.
func Buyers(customerID, partnerID, endUser string) []Buyer {
	var found []Buyer
	if endUser != "" {
		found = append(found, Buyer{customerID, endUser})
	}
	found = append(found, Buyer{customerID, ""})
	if partnerID != "" {
		found = append(found, Buyer{partnerID, ""})
	}
	return append(found, Buyer{})
}

type quantity struct {
	unit     string
	amount   int64
	required bool
}

func quantities(usage Usage) []quantity {
	all := []quantity{{"input_tokens", usage.InputUnits, true}, {"output_tokens", usage.OutputUnits, true}}
	if unit, ok := GeneratedUnit[usage.Modality]; ok {
		all = append(all, quantity{unit, usage.GeneratedUnits, true})
	}
	all = append(all, quantity{"requests", 1, false})
	kept := all[:0]
	for _, q := range all {
		if q.amount > 0 {
			kept = append(kept, q)
		}
	}
	return kept
}

type key struct{ provider, model, unit string }

// Price prices a request of the buyers at `at`.
func Price(rates []snapshot.Rate, providerKey, modelKey string, buyers []Buyer, at time.Time, usage Usage) []Charge {
	var charges []Charge
	for _, q := range quantities(usage) {
		k := key{providerKey, modelKey, q.unit}
		cost := costFor(rates, k, at, "")
		selling := sellingFor(rates, k, at, buyers)
		var price *decimal.Decimal
		if selling != nil {
			price = unitPrice(selling, rates, k, at)
		}
		// A percent covering every unit, like the standard price at cost, reaches the request
		// count too; with nothing to apply to there, the request is simply not charged for.
		if !q.required && cost == nil && price == nil {
			continue
		}
		charge := Charge{Unit: q.unit, Quantity: q.amount}
		if cost != nil {
			charge.CostRateID = cost.ID
		}
		if selling != nil {
			charge.SellingRateID = selling.ID
			charge.BillableCurrency = selling.Currency
			if price != nil {
				amount := decimal.NewFromInt(q.amount).Mul(*price).RoundBank(6)
				charge.Billable = &amount
			}
		}
		charges = append(charges, charge)
	}
	return charges
}

func covers(rate snapshot.Rate, k key) bool {
	return (rate.ProviderKey == k.provider || rate.ProviderKey == Any) &&
		(rate.ModelKey == k.model || rate.ModelKey == Any) &&
		(rate.Unit == k.unit || rate.Unit == Any)
}

func applies(rate snapshot.Rate, at time.Time) bool {
	return !rate.EffectiveFrom.After(at) && (rate.EffectiveTo == nil || rate.EffectiveTo.After(at))
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// moreSpecific orders a model before a whole provider before every provider, a unit named
// before "*", then the latest start.
func moreSpecific(a, b snapshot.Rate) bool {
	for _, named := range [][2]bool{
		{a.ModelKey != Any, b.ModelKey != Any},
		{a.ProviderKey != Any, b.ProviderKey != Any},
		{a.Unit != Any, b.Unit != Any},
	} {
		if named[0] != named[1] {
			return named[0]
		}
	}
	return a.EffectiveFrom.After(b.EffectiveFrom)
}

func sellingFor(rates []snapshot.Rate, k key, at time.Time, buyers []Buyer) *snapshot.Rate {
	for _, buyer := range buyers {
		var best *snapshot.Rate
		for i := range rates {
			rate := rates[i]
			if rate.Kind != "selling" || !applies(rate, at) || !covers(rate, k) {
				continue
			}
			if text(rate.OrganizationID) != buyer.Organization || text(rate.EndUser) != buyer.EndUser {
				continue
			}
			if best == nil || moreSpecific(rate, *best) {
				best = &rates[i]
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

// costFor is the provider's cost in force: negotiated before list, then the latest start.
func costFor(rates []snapshot.Rate, k key, at time.Time, costType string) *snapshot.Rate {
	var rows []snapshot.Rate
	for _, rate := range rates {
		exact := rate.ProviderKey == k.provider && rate.ModelKey == k.model && rate.Unit == k.unit
		typeMatches := costType == "" || text(rate.CostType) == costType
		if rate.Kind == "cost" && exact && applies(rate, at) && typeMatches {
			rows = append(rows, rate)
		}
	}
	slices.SortStableFunc(rows, func(a, b snapshot.Rate) int {
		aNegotiated, bNegotiated := text(a.CostType) == "negotiated", text(b.CostType) == "negotiated"
		if aNegotiated != bNegotiated {
			if aNegotiated {
				return -1
			}
			return 1
		}
		return b.EffectiveFrom.Compare(a.EffectiveFrom)
	})
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

func each(amount decimal.Decimal, perQuantity int64) decimal.Decimal {
	return amount.Div(decimal.NewFromInt(perQuantity))
}

// unitPrice is the selling price for one unit, exactly (round only the amount charged), or nil
// when what it is a percent of has no price (M03-O03).
func unitPrice(selling *snapshot.Rate, rates []snapshot.Rate, k key, at time.Time) *decimal.Decimal {
	if selling.Method == "fixed" || selling.Method == "custom" {
		if selling.Amount == nil {
			return nil
		}
		price := each(*selling.Amount, selling.PerQuantity)
		return &price
	}
	if selling.Percent == nil {
		return nil
	}
	var base *decimal.Decimal
	if selling.Method == "adjust" {
		standard := sellingFor(rates, k, at, []Buyer{{}})
		if standard != nil && standard.Method != "adjust" {
			base = unitPrice(standard, rates, k, at)
		}
	} else {
		costType := ""
		if selling.Method == "discount" {
			costType = "list"
		}
		if cost := costFor(rates, k, at, costType); cost != nil && cost.Amount != nil {
			paid := each(*cost.Amount, cost.PerQuantity)
			base = &paid
		}
	}
	if base == nil {
		return nil
	}
	hundred := decimal.NewFromInt(100)
	factor := decimal.NewFromInt(1).Add(selling.Percent.Div(hundred))
	if selling.Method == "discount" {
		factor = decimal.NewFromInt(1).Sub(selling.Percent.Div(hundred))
	}
	price := base.Mul(factor)
	return &price
}

// Billable totals the billable amount in one currency; mixed currencies count only `currency`.
func Billable(charges []Charge, currency string) decimal.Decimal {
	total := decimal.Zero
	for _, charge := range charges {
		if charge.Billable != nil && charge.BillableCurrency == currency {
			total = total.Add(*charge.Billable)
		}
	}
	return total
}
