// Package snapshottest builds configuration snapshots for tests: a partner with one customer,
// a direct customer, credentials, an end user, three providers and rates, shaped as the
// Console publishes them.
package snapshottest

import (
	"time"

	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/shopspring/decimal"
)

const (
	PartnerID        = "11111111-1111-1111-1111-111111111111"
	CustomerID       = "22222222-2222-2222-2222-222222222222"
	DirectCustomerID = "33333333-3333-3333-3333-333333333333"
	ClientID         = "client-acme"
	DirectClientID   = "client-direct"
	UserPoolID       = "us-east-1_TestPool"
	Scope            = "https://api.example/invoke"
)

func ptr[T any](v T) *T { return &v }

func money(value string) *decimal.Decimal {
	d := decimal.RequireFromString(value)
	return &d
}

// Sample is a fresh snapshot each call, so a test may change it.
func Sample() *snapshot.Snapshot {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &snapshot.Snapshot{
		General: snapshot.General{DefaultTimezone: "UTC", DefaultCurrency: "USD", SupportedCurrencies: []string{"USD"}},
		Regions: snapshot.Regions{ApprovedCountries: []string{"US", "DE", "GB"}, BlockedCountries: []string{"RU"}},
		Gateway: snapshot.GatewaySection{MaintenanceStatus: snapshot.MaintenanceOperational},
		Providers: &snapshot.ProviderCatalog{Providers: []snapshot.Provider{
			{Key: "openai", Name: "OpenAI", Endpoint: "https://api.openai.com/v1", Region: "us-east-1", Status: snapshot.StatusActive,
				KeyReference: ptr("essera/test/openai"), RateLimitRPM: ptr(600), Models: []snapshot.ProviderModel{
					{Key: "gpt-4o", DisplayName: "GPT-4o", Modality: "text", Status: snapshot.StatusActive},
					{Key: "gpt-old", DisplayName: "GPT old", Modality: "text", Status: snapshot.StatusDisabled},
				}},
			{Key: "bedrock", Name: "Amazon Bedrock", Endpoint: "https://bedrock-runtime.us-east-1.amazonaws.com", Region: "us-east-1", Status: snapshot.StatusActive,
				KeyReference: ptr("essera/test/bedrock"), Models: []snapshot.ProviderModel{
					{Key: "claude-sonnet", DisplayName: "Claude Sonnet", Modality: "text", Status: snapshot.StatusActive},
					{Key: "gpt-4o", DisplayName: "GPT-4o on Bedrock", Modality: "text", Status: snapshot.StatusActive},
				}},
			{Key: "acme-llm", Name: "Acme LLM", Endpoint: "https://llm.acme.example/v1", Region: "eu-west-1", Status: snapshot.StatusActive,
				Models: []snapshot.ProviderModel{{Key: "acme-1", DisplayName: "Acme 1", Modality: "text", Status: snapshot.StatusActive}}},
		}},
		Accounts: &snapshot.AccountsCatalog{
			Partners: []snapshot.Organization{{ID: PartnerID, Status: snapshot.StatusActive, Entitlements: snapshot.Entitlements{
				MonthlyRequestQuota: ptr(int64(1000)),
			}}},
			Customers: []snapshot.Organization{
				{ID: CustomerID, PartnerID: ptr(PartnerID), Status: snapshot.StatusActive, Entitlements: snapshot.Entitlements{
					Countries: []string{"US", "DE"}, Providers: []string{"openai", "bedrock"},
					Models:              []string{"openai/gpt-4o", "openai/gpt-old", "bedrock/claude-sonnet"},
					MonthlyRequestQuota: ptr(int64(1000)), MonthlySpendLimit: money("50.00"), Currency: ptr("USD"),
				}},
				{ID: DirectCustomerID, Status: snapshot.StatusActive},
			},
			EndUsers: []snapshot.EndUser{
				{ID: "44444444-4444-4444-4444-444444444444", CustomerID: CustomerID, ExternalID: "user-1", Status: snapshot.StatusActive,
					Limits: snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(2))}},
				{ID: "55555555-5555-5555-5555-555555555555", CustomerID: CustomerID, ExternalID: "user-off", Status: "suspended"},
			},
			Credentials: []snapshot.Credential{
				{ClientID: ClientID, CustomerID: CustomerID},
				{ClientID: DirectClientID, CustomerID: DirectCustomerID},
			},
		},
		Rates: &snapshot.RatesCatalog{Rates: []snapshot.Rate{
			{ID: "cost-list", Kind: "cost", ProviderKey: "openai", ModelKey: "gpt-4o", Unit: "input_tokens", PerQuantity: 1_000_000,
				CostType: ptr("list"), Method: "fixed", Amount: money("2.50"), Currency: "USD", EffectiveFrom: from},
			{ID: "cost-negotiated", Kind: "cost", ProviderKey: "openai", ModelKey: "gpt-4o", Unit: "input_tokens", PerQuantity: 1_000_000,
				CostType: ptr("negotiated"), Method: "fixed", Amount: money("2.00"), Currency: "USD", EffectiveFrom: from},
			{ID: "sell-partner", Kind: "selling", ProviderKey: "openai", ModelKey: "gpt-4o", Unit: "input_tokens", PerQuantity: 1_000_000,
				OrganizationID: ptr(PartnerID), Method: "markup", Percent: money("20"), Currency: "USD", EffectiveFrom: from},
			{ID: "sell-customer", Kind: "selling", ProviderKey: "openai", ModelKey: "gpt-4o", Unit: "output_tokens", PerQuantity: 1_000_000,
				OrganizationID: ptr(CustomerID), Method: "fixed", Amount: money("12.00"), Currency: "USD", EffectiveFrom: from},
		}},
	}
}

// View indexes Sample as version 7.
func View() *snapshot.View { return snapshot.NewView(7, Sample()) }
