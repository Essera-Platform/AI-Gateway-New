// Package snapshottest builds configuration snapshots for tests: a partner with one customer,
// a direct customer, API keys, end users, people, three providers and rates, shaped as the
// Console publishes them.
package snapshottest

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/shopspring/decimal"
)

const (
	PartnerID        = "11111111-1111-1111-1111-111111111111"
	CustomerID       = "22222222-2222-2222-2222-222222222222"
	DirectCustomerID = "33333333-3333-3333-3333-333333333333"
	UserPoolID       = "us-east-1_TestPool" // the pool people sign in with

	EndUserID = "44444444-4444-4444-4444-444444444444" // external id "user-1"

	// People: the admin and a user of CustomerID, a suspended person, and the
	// admin of the direct customer, another tenant.
	AdminPersonID     = "66666666-6666-6666-6666-666666666666"
	UserPersonID      = "77777777-7777-7777-7777-777777777777"
	SuspendedPersonID = "88888888-8888-8888-8888-888888888888"
	DirectPersonID    = "99999999-9999-9999-9999-999999999999"
	AdminSub          = "sub-admin"
	UserSub           = "sub-user"
	SuspendedSub      = "sub-suspended"
	DirectSub         = "sub-direct"

	// APIKey is the end user's key, published as KeyID with its SHA-256. AdminKeyID and
	// DirectKeyID are the keys of the two customers' admins, with no limits of their own.
	APIKey      = "sk-0123456789abcdefAbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCd"
	KeyID       = "k_0123456789abcdef"
	AdminKeyID  = "k_1111111111111111"
	DirectKeyID = "k_2222222222222222"
)

// SHA256 is the hex SHA-256 the Console publishes for a key.
func SHA256(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

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
				{ID: EndUserID, CustomerID: CustomerID, ExternalID: "user-1", Status: snapshot.StatusActive,
					Limits: snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(2))}},
				{ID: "55555555-5555-5555-5555-555555555555", CustomerID: CustomerID, ExternalID: "user-off", Status: "suspended"},
			},
			Credentials: []snapshot.Credential{
				{Kind: snapshot.CredentialAPIKey, KeyID: AdminKeyID, KeySHA256: SHA256("admin"), CustomerID: CustomerID,
					PersonID: AdminPersonID},
				{Kind: snapshot.CredentialAPIKey, KeyID: DirectKeyID, KeySHA256: SHA256("direct"), CustomerID: DirectCustomerID,
					PersonID: DirectPersonID},
				{Kind: snapshot.CredentialAPIKey, KeyID: KeyID, KeySHA256: SHA256(APIKey), CustomerID: CustomerID,
					PersonID: UserPersonID, EndUserID: ptr(EndUserID), ExpiresAt: ptr(time.Date(2027, 2, 17, 0, 0, 0, 0, time.UTC)),
					Limits: snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(2))}},
			},
			People: []snapshot.Person{
				{ID: AdminPersonID, Sub: ptr(AdminSub), Email: "owner@acme.example", CustomerID: CustomerID,
					Role: snapshot.RoleAdmin, Status: snapshot.StatusActive},
				{ID: UserPersonID, Sub: ptr(UserSub), Email: "user-1@acme.example", CustomerID: CustomerID,
					EndUserID: ptr(EndUserID), EndUserExternalID: ptr("user-1"), Role: snapshot.RoleUser, Status: snapshot.StatusActive},
				{ID: SuspendedPersonID, Sub: ptr(SuspendedSub), Email: "gone@acme.example", CustomerID: CustomerID,
					Role: snapshot.RoleUser, Status: "suspended"},
				{ID: DirectPersonID, Sub: ptr(DirectSub), Email: "owner@direct.example", CustomerID: DirectCustomerID,
					Role: snapshot.RoleAdmin, Status: snapshot.StatusActive},
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
