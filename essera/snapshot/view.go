package snapshot

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

// View is one applied version with the lookups a request needs.
type View struct {
	Version  int64
	Snapshot *Snapshot

	partners    map[string]*Organization
	customers   map[string]*Organization
	credentials map[string]*Credential
	endUsers    map[endUserKey]*EndUser
	providers   map[string]*Provider
	models      map[string]*ProviderModel // provider_key/model_key
	location    *time.Location
}

type endUserKey struct {
	customerID string
	externalID string
}

var current atomic.Pointer[View]

// Current is the version requests are served with; nil until one is applied.
func Current() *View { return current.Load() }

// Activate makes view the version every following request is served with.
func Activate(view *View) { current.Store(view) }

// Parse decodes a snapshot document.
func Parse(data []byte) (*Snapshot, error) {
	var parsed Snapshot
	if err := common.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	if parsed.Gateway.MaintenanceStatus == "" || parsed.General.DefaultCurrency == "" {
		return nil, errors.New("the snapshot lacks its gateway or general section")
	}
	return &parsed, nil
}

// Merge returns next with every section it omits taken from previous: a version written
// before a module published its catalog leaves that catalog unchanged.
func Merge(previous, next *Snapshot) *Snapshot {
	if previous == nil {
		return next
	}
	merged := *next
	if merged.Providers == nil {
		merged.Providers = previous.Providers
	}
	if merged.Accounts == nil {
		merged.Accounts = previous.Accounts
	}
	if merged.Rates == nil {
		merged.Rates = previous.Rates
	}
	return &merged
}

// NewView indexes a snapshot.
func NewView(version int64, s *Snapshot) *View {
	view := &View{
		Version:     version,
		Snapshot:    s,
		partners:    map[string]*Organization{},
		customers:   map[string]*Organization{},
		credentials: map[string]*Credential{},
		endUsers:    map[endUserKey]*EndUser{},
		providers:   map[string]*Provider{},
		models:      map[string]*ProviderModel{},
		location:    time.UTC,
	}
	if location, err := time.LoadLocation(s.General.DefaultTimezone); err == nil {
		view.location = location
	}
	if s.Accounts != nil {
		for i := range s.Accounts.Partners {
			view.partners[s.Accounts.Partners[i].ID] = &s.Accounts.Partners[i]
		}
		for i := range s.Accounts.Customers {
			view.customers[s.Accounts.Customers[i].ID] = &s.Accounts.Customers[i]
		}
		for i := range s.Accounts.Credentials {
			view.credentials[s.Accounts.Credentials[i].ClientID] = &s.Accounts.Credentials[i]
		}
		for i := range s.Accounts.EndUsers {
			user := &s.Accounts.EndUsers[i]
			view.endUsers[endUserKey{user.CustomerID, user.ExternalID}] = user
		}
	}
	if s.Providers != nil {
		for i := range s.Providers.Providers {
			provider := &s.Providers.Providers[i]
			view.providers[provider.Key] = provider
			for j := range provider.Models {
				view.models[provider.Key+"/"+provider.Models[j].Key] = &provider.Models[j]
			}
		}
	}
	return view
}

func (v *View) Credential(clientID string) *Credential { return v.credentials[clientID] }
func (v *View) Customer(id string) *Organization       { return v.customers[id] }
func (v *View) Provider(key string) *Provider          { return v.providers[key] }
func (v *View) Location() *time.Location               { return v.location }

func (v *View) Partner(id *string) *Organization {
	if id == nil {
		return nil
	}
	return v.partners[*id]
}

func (v *View) EndUser(customerID, externalID string) *EndUser {
	return v.endUsers[endUserKey{customerID, externalID}]
}

// Providers lists the catalog's providers in their published order.
func (v *View) Providers() []Provider {
	if v.Snapshot.Providers == nil {
		return nil
	}
	return v.Snapshot.Providers.Providers
}

// ModelServable reports whether the provider and its model are both active.
func (v *View) ModelServable(providerKey, modelKey string) bool {
	provider := v.providers[providerKey]
	model := v.models[providerKey+"/"+modelKey]
	return provider != nil && model != nil && provider.Status == StatusActive && model.Status == StatusActive
}

// Narrow applies a child's own restrictions to its parent's: a list keeps only what both
// allow, a limit takes the lower value, anything else the child sets replaces the parent's.
// The Console guarantees a child never widens its parent; intersecting keeps that true even
// for a stale or inconsistent version.
func Narrow(parent Entitlements, own Entitlements) Entitlements {
	result := parent
	result.Countries = narrowList(parent.Countries, own.Countries)
	result.Providers = narrowList(parent.Providers, own.Providers)
	result.Models = narrowList(parent.Models, own.Models)
	result.MonthlyRequestQuota = lowerInt(parent.MonthlyRequestQuota, own.MonthlyRequestQuota)
	result.MonthlyTokenQuota = lowerInt(parent.MonthlyTokenQuota, own.MonthlyTokenQuota)
	result.MonthlySpendLimit = lowerDecimal(parent.MonthlySpendLimit, own.MonthlySpendLimit)
	if own.Currency != nil {
		result.Currency = own.Currency
	}
	if own.BillingCycle != nil {
		result.BillingCycle = own.BillingCycle
	}
	if own.PricingPlan != nil {
		result.PricingPlan = own.PricingPlan
	}
	return result
}

func narrowList(parent, own []string) []string {
	if own == nil {
		return parent
	}
	if parent == nil {
		return own
	}
	kept := make([]string, 0, len(own))
	for _, value := range own {
		if slices.Contains(parent, value) {
			kept = append(kept, value)
		}
	}
	return kept
}

func lowerInt(parent, own *int64) *int64 {
	if own == nil {
		return parent
	}
	if parent == nil || *own < *parent {
		return own
	}
	return parent
}

func lowerDecimal(parent, own *decimal.Decimal) *decimal.Decimal {
	if own == nil {
		return parent
	}
	if parent == nil || own.LessThan(*parent) {
		return own
	}
	return parent
}

// Currency is the currency of the entitlements' spend limit.
func (v *View) Currency(e Entitlements) string {
	if e.Currency != nil && *e.Currency != "" {
		return *e.Currency
	}
	return v.Snapshot.General.DefaultCurrency
}

// CountryAllowed applies the platform's blocked and approved lists and then the account's
// own list. country is an ISO 3166-1 alpha-2 code, or "" when unknown.
func (v *View) CountryAllowed(e Entitlements, country string) (allowed bool, reason string) {
	country = strings.ToUpper(strings.TrimSpace(country))
	regions := v.Snapshot.Regions
	listed := len(regions.ApprovedCountries) > 0 || e.Countries != nil
	if country == "" || country == "XX" || country == "T1" {
		if listed || len(regions.BlockedCountries) > 0 {
			return false, "essera_country_unknown"
		}
		return true, ""
	}
	if slices.Contains(regions.BlockedCountries, country) {
		return false, "essera_country_denied"
	}
	if len(regions.ApprovedCountries) > 0 && !slices.Contains(regions.ApprovedCountries, country) {
		return false, "essera_country_denied"
	}
	if e.Countries != nil && !slices.Contains(e.Countries, country) {
		return false, "essera_country_denied"
	}
	return true, ""
}
