package snapshot_test

import (
	"testing"

	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestParseReadsTheConsolesDocument(t *testing.T) {
	// The Console writes decimals as strings and may add fields this Gateway does not know, or
	// still send ones it no longer reads, such as an older snapshot's pricing_plan.
	document := []byte(`{"general":{"default_timezone":"Europe/Berlin","default_currency":"EUR","supported_currencies":["EUR"]},` +
		`"gateway":{"maintenance_status":"operational","message":"","window_start":null,"window_end":null},` +
		`"regions":{"approved_countries":[],"blocked_countries":[]},"billing":{},"notifications":{"routes":[]},"retention":{},"contacts":{"emergency":[]},` +
		`"accounts":{"partners":[],"customers":[{"id":"c","partner_id":null,"status":"active","entitlements":{"monthly_spend_limit":"12000.00","currency":null,"pricing_plan":"Enterprise"}}],` +
		`"end_users":[],"credentials":[],"a_field_added_later":true}}`)

	parsed, err := snapshot.Parse(document)

	require.NoError(t, err)
	assert.Equal(t, "EUR", parsed.General.DefaultCurrency)
	assert.Nil(t, parsed.Providers, "an absent section stays absent")
	assert.True(t, decimal.RequireFromString("12000").Equal(*parsed.Accounts.Customers[0].Entitlements.MonthlySpendLimit))
	assert.Equal(t, "Europe/Berlin", snapshot.NewView(1, parsed).Location().String())
}

func TestParseRefusesADocumentWithoutItsSections(t *testing.T) {
	_, err := snapshot.Parse([]byte(`{"providers":{"providers":[]}}`))
	assert.Error(t, err)
}

func TestMergeKeepsCatalogsAVersionOmits(t *testing.T) {
	previous := snapshottest.Sample()
	next := snapshottest.Sample()
	next.Providers, next.Rates = nil, nil
	next.Gateway.MaintenanceStatus = snapshot.MaintenanceActive

	merged := snapshot.Merge(previous, next)

	assert.Same(t, previous.Providers, merged.Providers)
	assert.Same(t, previous.Rates, merged.Rates)
	assert.Same(t, next.Accounts, merged.Accounts)
	assert.Equal(t, snapshot.MaintenanceActive, merged.Gateway.MaintenanceStatus)
}

func TestNarrowNeverWidensTheParent(t *testing.T) {
	parent := snapshot.Entitlements{
		Countries: []string{"US", "DE"}, Models: []string{"a/x", "a/y"},
		MonthlyRequestQuota: ptr(int64(100)), MonthlySpendLimit: ptr(decimal.RequireFromString("50")),
	}
	own := snapshot.Entitlements{
		Countries: []string{"DE", "FR"}, MonthlyRequestQuota: ptr(int64(500)), MonthlyTokenQuota: ptr(int64(9)),
		MonthlySpendLimit: ptr(decimal.RequireFromString("20")), Currency: ptr("EUR"),
	}

	narrowed := snapshot.Narrow(parent, own)

	assert.Equal(t, []string{"DE"}, narrowed.Countries)
	assert.Equal(t, []string{"a/x", "a/y"}, narrowed.Models, "unset inherits")
	assert.Equal(t, int64(100), *narrowed.MonthlyRequestQuota, "a child's higher limit does not apply")
	assert.Equal(t, int64(9), *narrowed.MonthlyTokenQuota)
	assert.True(t, decimal.RequireFromString("20").Equal(*narrowed.MonthlySpendLimit))
	assert.Equal(t, "EUR", *narrowed.Currency)
}

func TestCountryAllowed(t *testing.T) {
	view := snapshottest.View()
	account := snapshot.Entitlements{Countries: []string{"US", "DE"}}
	cases := []struct {
		name     string
		regions  snapshot.Regions
		account  snapshot.Entitlements
		country  string
		allowed  bool
		wantCode string
	}{
		{"approved and in the account's list", view.Snapshot.Regions, account, "de", true, ""},
		{"blocked everywhere", view.Snapshot.Regions, snapshot.Entitlements{}, "RU", false, "essera_country_denied"},
		{"not on the platform's approved list", view.Snapshot.Regions, snapshot.Entitlements{}, "FR", false, "essera_country_denied"},
		{"approved but not for the account", view.Snapshot.Regions, account, "GB", false, "essera_country_denied"},
		{"unknown while a list applies", view.Snapshot.Regions, account, "", false, "essera_country_unknown"},
		{"Cloudflare's unknown code", view.Snapshot.Regions, account, "XX", false, "essera_country_unknown"},
		{"unknown and no list at all", snapshot.Regions{}, snapshot.Entitlements{}, "", true, ""},
		{"any country without lists", snapshot.Regions{}, snapshot.Entitlements{}, "BR", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view.Snapshot.Regions = tc.regions
			allowed, code := view.CountryAllowed(tc.account, tc.country)
			assert.Equal(t, tc.allowed, allowed)
			assert.Equal(t, tc.wantCode, code)
		})
	}
}

func TestModelServableNeedsBothActive(t *testing.T) {
	view := snapshottest.View()
	assert.True(t, view.ModelServable("openai", "gpt-4o"))
	assert.False(t, view.ModelServable("openai", "gpt-old"), "a disabled model")
	assert.False(t, view.ModelServable("openai", "missing"))
	view.Snapshot.Providers.Providers[0].Status = snapshot.StatusMaintenance
	view = snapshot.NewView(8, view.Snapshot)
	assert.False(t, view.ModelServable("openai", "gpt-4o"), "a provider in maintenance")
}
