package policy

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/essera/snapshot"
)

// Caching is how much prompt caching a request may ask the provider for: the Console bills
// cache reads and writes as their own units, so a request may cache only where they are priced.
type Caching int

const (
	CachingOff         Caching = iota // no rates for the cache units: caching markers are dropped
	CachingFiveMinutes                // reads and 5-minute writes priced; a 1-hour TTL is dropped
	CachingAll                        // the 1-hour writes are priced too
)

const anyKey = "*"

// PromptCaching is the caching the request may use for a model: a unit counts as priced when
// the snapshot has a cost rate for it and a selling rate that covers it for the first buyer
// with one (the end user, the customer, its partner, the standard price), as the Console
// rates. A model with long-context costs needs its long-context cache units priced as well.
func (r *Request) PromptCaching(providerKey, modelKey string, now time.Time) Caching {
	if r == nil || r.Caller == nil || r.Caller.View == nil || r.Caller.View.Snapshot.Rates == nil {
		return CachingOff
	}
	rates := r.Caller.View.Snapshot.Rates.Rates
	long := false
	for _, rate := range rates {
		if rate.Kind == "cost" && rate.ProviderKey == providerKey && rate.ModelKey == modelKey && strings.HasSuffix(rate.Unit, "_long") && inForce(rate, now) {
			long = true
		}
	}
	priced := func(unit string) bool {
		if !r.priced(rates, providerKey, modelKey, unit, now) {
			return false
		}
		return !long || r.priced(rates, providerKey, modelKey, unit+"_long", now)
	}
	if !priced("cache_read_tokens") || !priced("cache_write_tokens") {
		return CachingOff
	}
	if !priced("cache_write_1h_tokens") {
		return CachingFiveMinutes
	}
	return CachingAll
}

func (r *Request) priced(rates []snapshot.Rate, providerKey, modelKey, unit string, now time.Time) bool {
	cost := false
	for _, rate := range rates {
		if rate.Kind == "cost" && rate.ProviderKey == providerKey && rate.ModelKey == modelKey && rate.Unit == unit && rate.Amount != nil && inForce(rate, now) {
			cost = true
			break
		}
	}
	if !cost {
		return false
	}
	for _, buyer := range r.buyers() {
		for _, rate := range rates {
			if rate.Kind != "selling" || !inForce(rate, now) || !sameBuyer(rate, buyer) {
				continue
			}
			if covers(rate.ProviderKey, providerKey) && covers(rate.ModelKey, modelKey) && covers(rate.Unit, unit) {
				return true
			}
		}
	}
	return false
}

func (r *Request) buyers() [][2]string {
	customer := r.Caller.Customer
	if customer == nil {
		return [][2]string{{"", ""}}
	}
	buyers := [][2]string{}
	if r.EndUserID != "" {
		buyers = append(buyers, [2]string{customer.ID, r.EndUserID})
	}
	buyers = append(buyers, [2]string{customer.ID, ""})
	if r.Caller.Partner != nil {
		buyers = append(buyers, [2]string{r.Caller.Partner.ID, ""})
	}
	return append(buyers, [2]string{"", ""})
}

func sameBuyer(rate snapshot.Rate, buyer [2]string) bool {
	organization, endUser := "", ""
	if rate.OrganizationID != nil {
		organization = *rate.OrganizationID
	}
	if rate.EndUser != nil {
		endUser = *rate.EndUser
	}
	return organization == buyer[0] && endUser == buyer[1]
}

func covers(rateKey, key string) bool { return rateKey == key || rateKey == anyKey }

func inForce(rate snapshot.Rate, now time.Time) bool {
	return !rate.EffectiveFrom.After(now) && (rate.EffectiveTo == nil || rate.EffectiveTo.After(now))
}
