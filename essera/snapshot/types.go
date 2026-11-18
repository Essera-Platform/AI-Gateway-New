// Package snapshot holds the configuration the Essera Operations Console publishes: the Go
// shape of docs/handoff/event-contracts/configuration-snapshot.schema.json in the Console
// repository, and the in-memory view every request is served with.
package snapshot

import (
	"time"

	"github.com/shopspring/decimal"
)

// Snapshot is one immutable configuration version. The seven M12 sections are always
// present; providers, accounts and rates are absent in versions written before their module
// published them, which means "unchanged", not "empty".
type Snapshot struct {
	General       General          `json:"general"`
	Billing       Billing          `json:"billing"`
	Notifications Notifications    `json:"notifications"`
	Regions       Regions          `json:"regions"`
	Retention     Retention        `json:"retention"`
	Contacts      Contacts         `json:"contacts"`
	Gateway       GatewaySection   `json:"gateway"`
	Providers     *ProviderCatalog `json:"providers,omitempty"`
	Accounts      *AccountsCatalog `json:"accounts,omitempty"`
	Rates         *RatesCatalog    `json:"rates,omitempty"`
}

type General struct {
	DefaultTimezone     string   `json:"default_timezone"`
	DefaultCurrency     string   `json:"default_currency"`
	SupportedCurrencies []string `json:"supported_currencies"`
}

type Billing struct {
	Cycle                    string `json:"cycle"`
	CycleAnchorDay           int    `json:"cycle_anchor_day"`
	UsageWarningThresholdPct int    `json:"usage_warning_threshold_pct"`
	SpendWarningThresholdPct int    `json:"spend_warning_threshold_pct"`
}

type Notifications struct {
	Routes []NotificationRoute `json:"routes"`
}

type NotificationRoute struct {
	Topic     string `json:"topic"`
	Channel   string `json:"channel"`
	Recipient string `json:"recipient"`
}

type Regions struct {
	ApprovedCountries []string `json:"approved_countries"`
	BlockedCountries  []string `json:"blocked_countries"`
}

type Retention struct {
	AuditLogDays      int `json:"audit_log_days"`
	UsageDataDays     int `json:"usage_data_days"`
	ExportFileDays    int `json:"export_file_days"`
	SessionRecordDays int `json:"session_record_days"`
}

type Contacts struct {
	SupportEmail *string   `json:"support_email"`
	SupportPhone *string   `json:"support_phone"`
	Emergency    []Contact `json:"emergency"`
}

type Contact struct {
	Name  string  `json:"name"`
	Role  string  `json:"role"`
	Email *string `json:"email"`
	Phone *string `json:"phone"`
}

const (
	MaintenanceOperational = "operational"
	MaintenanceScheduled   = "scheduled"
	MaintenanceActive      = "in_maintenance"
)

type GatewaySection struct {
	MaintenanceStatus string     `json:"maintenance_status"`
	Message           string     `json:"message"`
	WindowStart       *time.Time `json:"window_start"`
	WindowEnd         *time.Time `json:"window_end"`
}

type ProviderCatalog struct {
	Providers []Provider `json:"providers"`
}

const (
	StatusActive      = "active"
	StatusDisabled    = "disabled"
	StatusMaintenance = "maintenance"
)

type Provider struct {
	Key               string          `json:"key"`
	Name              string          `json:"name"`
	Endpoint          string          `json:"endpoint"`
	Region            string          `json:"region"`
	TimeoutMs         int             `json:"timeout_ms"`
	MaxRetries        int             `json:"max_retries"`
	RateLimitRPM      *int            `json:"rate_limit_rpm"`
	FallbackPolicyRef *string         `json:"fallback_policy_ref"`
	Status            string          `json:"status"`
	KeyReference      *string         `json:"key_reference"`
	Models            []ProviderModel `json:"models"`
}

type ProviderModel struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Modality    string `json:"modality"`
	Status      string `json:"status"`
}

type AccountsCatalog struct {
	Partners    []Organization `json:"partners"`
	Customers   []Organization `json:"customers"`
	EndUsers    []EndUser      `json:"end_users"`
	Credentials []Credential   `json:"credentials"`
}

type Organization struct {
	ID           string       `json:"id"`
	PartnerID    *string      `json:"partner_id"`
	Status       string       `json:"status"`
	Entitlements Entitlements `json:"entitlements"`
}

type EndUser struct {
	ID         string       `json:"id"`
	CustomerID string       `json:"customer_id"`
	ExternalID string       `json:"external_id"`
	Status     string       `json:"status"`
	Limits     Entitlements `json:"limits"`
}

type Credential struct {
	ClientID   string       `json:"client_id"`
	CustomerID string       `json:"customer_id"`
	ExpiresAt  *time.Time   `json:"expires_at"`
	Limits     Entitlements `json:"limits"`
}

// Entitlements are one level's restrictions; nil means "as the parent permits".
type Entitlements struct {
	Countries           []string         `json:"countries"`
	Providers           []string         `json:"providers"`
	Models              []string         `json:"models"`
	MonthlyRequestQuota *int64           `json:"monthly_request_quota"`
	MonthlyTokenQuota   *int64           `json:"monthly_token_quota"`
	MonthlySpendLimit   *decimal.Decimal `json:"monthly_spend_limit"`
	Currency            *string          `json:"currency"`
	BillingCycle        *string          `json:"billing_cycle"`
	PricingPlan         *string          `json:"pricing_plan"`
}

type RatesCatalog struct {
	Rates []Rate `json:"rates"`
}

type Rate struct {
	ID             string           `json:"id"`
	Kind           string           `json:"kind"`
	ProviderKey    string           `json:"provider_key"`
	ModelKey       string           `json:"model_key"`
	Unit           string           `json:"unit"`
	PerQuantity    int64            `json:"per_quantity"`
	CostType       *string          `json:"cost_type"`
	OrganizationID *string          `json:"organization_id"`
	EndUser        *string          `json:"end_user_external_id"`
	Method         string           `json:"method"`
	Amount         *decimal.Decimal `json:"amount"`
	Percent        *decimal.Decimal `json:"percent"`
	Currency       string           `json:"currency"`
	EffectiveFrom  time.Time        `json:"effective_from"`
	EffectiveTo    *time.Time       `json:"effective_to"`
}

// Event is the detail of the configuration.updated event that announces a version.
type Event struct {
	Version        int64    `json:"version"`
	SnapshotKey    string   `json:"snapshot_key"`
	CorrelationID  *string  `json:"correlation_id"`
	ChangedModules []string `json:"changed_modules"`
	Bucket         string   `json:"bucket"`
	SHA256         string   `json:"sha256"`
	Environment    string   `json:"environment"`
}
