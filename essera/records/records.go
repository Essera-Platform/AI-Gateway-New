// Package records holds the Gateway's own tables for the Essera integration. They are working
// state only: every business fact lives in the Console and reaches the Gateway in snapshots.
package records

import (
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// ConfigVersion is one configuration version the Gateway received, with its document so the
// last good one is served after a restart even when S3 or SQS cannot be reached.
type ConfigVersion struct {
	Version   int64  `gorm:"primaryKey;autoIncrement:false"`
	SHA256    string `gorm:"type:varchar(64)"`
	Document  string `gorm:"type:text"`
	Applied   bool
	Error     string `gorm:"type:text"`
	CreatedAt time.Time
}

func (ConfigVersion) TableName() string { return "essera_config_versions" }

// OutboxEvent is a usage event not yet accepted by the usage queue.
type OutboxEvent struct {
	ID        int64  `gorm:"primaryKey"`
	EventID   string `gorm:"type:varchar(128);uniqueIndex"`
	Body      string `gorm:"type:text"`
	Attempts  int
	CreatedAt time.Time `gorm:"index"`
}

func (OutboxEvent) TableName() string { return "essera_usage_outbox" }

// Counter is what one subject used in one month, for its monthly limits.
type Counter struct {
	Scope    string `gorm:"primaryKey;type:varchar(16)"`  // partner, customer, credential, end_user
	Subject  string `gorm:"primaryKey;type:varchar(256)"` // its id, client id or customer/external id
	Month    string `gorm:"primaryKey;type:varchar(7)"`   // 2026-11, in the platform's timezone
	Currency string `gorm:"primaryKey;type:varchar(3)"`
	Requests int64
	Tokens   int64
	Spend    decimal.Decimal `gorm:"type:numeric(20,6)"`
}

func (Counter) TableName() string { return "essera_usage_counters" }

// RequestStat aggregates the requests of one five-minute bucket, provider, model and outcome
// for telemetry. Latency is a histogram over fixed bucket edges.
type RequestStat struct {
	Bucket     int64  `gorm:"primaryKey;autoIncrement:false"` // unix seconds, start of the bucket
	Provider   string `gorm:"primaryKey;type:varchar(64)"`
	Model      string `gorm:"primaryKey;type:varchar(200)"`
	Outcome    string `gorm:"primaryKey;type:varchar(32)"` // succeeded, failed, timeout, rate_limited, denied:<reason>
	HTTPStatus int    `gorm:"primaryKey;autoIncrement:false"`
	Requests   int64
	Retries    int64
	LatencySum int64  // milliseconds
	Histogram  string `gorm:"type:text"` // JSON array of counts per edge in stats.LatencyEdges
}

func (RequestStat) TableName() string { return "essera_request_stats" }

// ProviderError is one recent provider error, kept for telemetry for a day.
type ProviderError struct {
	ID         int64     `gorm:"primaryKey"`
	At         time.Time `gorm:"index"`
	Provider   string    `gorm:"type:varchar(64)"`
	Model      string    `gorm:"type:varchar(200)"`
	HTTPStatus int
	Message    string `gorm:"type:varchar(500)"`
}

func (ProviderError) TableName() string { return "essera_provider_errors" }

// Migrate creates or updates the Essera tables in the primary database.
func Migrate() error {
	return model.DB.AutoMigrate(&ConfigVersion{}, &OutboxEvent{}, &Counter{}, &RequestStat{}, &ProviderError{})
}
