// Package rating prices a request with the rates in the configuration, as the Console does
// (apps/backend/app/modules/usage/rating.py and rates/rules.py in the Console repository).
// The Console's ledger stays the record of what a request cost; the Gateway prices only to
// enforce spend limits and to name the rate versions it applied on the usage event.
package rating

import (
	"slices"
	"time"

	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/shopspring/decimal"
)

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

// Price prices a request of the buyers (the customer, then its partner) at `at`.
func Price(rates []snapshot.Rate, providerKey, modelKey string, buyers []string, at time.Time, usage Usage) []Charge {
	var charges []Charge
	for _, q := range quantities(usage) {
		started := startedRates(rates, providerKey, modelKey, q.unit, at)
		cost := preferred(started, "cost", at, nil, "")
		selling := preferred(started, "selling", at, buyers, "")
		if cost == nil && selling == nil && !q.required {
			continue
		}
		charge := Charge{Unit: q.unit, Quantity: q.amount}
		if cost != nil {
			charge.CostRateID = cost.ID
		}
		if selling != nil {
			charge.SellingRateID = selling.ID
			charge.BillableCurrency = selling.Currency
			reference := cost
			if selling.Method == "discount" || selling.Method == "markup" {
				costType := ""
				if selling.Method == "discount" {
					costType = "list"
				}
				reference = preferred(started, "cost", at, nil, costType)
			}
			if price := unitPrice(selling, reference); price != nil {
				amount := decimal.NewFromInt(q.amount).Mul(*price).Div(decimal.NewFromInt(selling.PerQuantity)).RoundBank(6)
				charge.Billable = &amount
			}
		}
		charges = append(charges, charge)
	}
	return charges
}

func startedRates(rates []snapshot.Rate, providerKey, modelKey, unit string, at time.Time) []snapshot.Rate {
	var started []snapshot.Rate
	for _, rate := range rates {
		if rate.ProviderKey == providerKey && rate.ModelKey == modelKey && rate.Unit == unit && !rate.EffectiveFrom.After(at) {
			started = append(started, rate)
		}
	}
	return started
}

// preferred is the version in force: a cost rate prefers negotiated over list, then the
// latest start; a selling rate prefers the first buyer that has one, then the latest start.
func preferred(started []snapshot.Rate, kind string, at time.Time, buyers []string, costType string) *snapshot.Rate {
	var rows []snapshot.Rate
	for _, rate := range started {
		inForce := rate.EffectiveTo == nil || rate.EffectiveTo.After(at)
		typeMatches := costType == "" || (rate.CostType != nil && *rate.CostType == costType)
		if rate.Kind == kind && inForce && typeMatches {
			rows = append(rows, rate)
		}
	}
	if kind == "cost" {
		slices.SortStableFunc(rows, func(a, b snapshot.Rate) int {
			aNegotiated := a.CostType != nil && *a.CostType == "negotiated"
			bNegotiated := b.CostType != nil && *b.CostType == "negotiated"
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
	for _, buyer := range buyers {
		var best *snapshot.Rate
		for i := range rows {
			if rows[i].OrganizationID != nil && *rows[i].OrganizationID == buyer {
				if best == nil || rows[i].EffectiveFrom.After(best.EffectiveFrom) {
					best = &rows[i]
				}
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

// unitPrice is the selling price per per_quantity units by its method (M03-O03).
func unitPrice(selling *snapshot.Rate, cost *snapshot.Rate) *decimal.Decimal {
	if selling.Method == "fixed" || selling.Method == "custom" {
		return selling.Amount
	}
	if cost == nil || cost.Amount == nil || selling.Percent == nil {
		return nil
	}
	base := cost.Amount.Mul(decimal.NewFromInt(selling.PerQuantity)).Div(decimal.NewFromInt(cost.PerQuantity))
	hundred := decimal.NewFromInt(100)
	factor := decimal.NewFromInt(1).Add(selling.Percent.Div(hundred))
	if selling.Method == "discount" {
		factor = decimal.NewFromInt(1).Sub(selling.Percent.Div(hundred))
	}
	price := base.Mul(factor).RoundBank(6)
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
