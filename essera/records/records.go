// Package records holds the Gateway's own tables for the Essera integration. They are working
// state only: every business fact lives in the Console and reaches the Gateway in snapshots.
package records

import (
	"time"

	"github.com/QuantumNous/new-api/model"
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

// ConsoleTotal is what one subject used in one month as the Console last published it
// (usage.totals): the only source of its spend.
type ConsoleTotal struct {
	Scope    string `gorm:"primaryKey;type:varchar(16)"`  // partner, customer, credential, end_user
	Subject  string `gorm:"primaryKey;type:varchar(400)"` // its id, client or key id, or customer/external id
	Month    string `gorm:"primaryKey;type:varchar(7)"`   // 2026-11, in the platform's timezone
	Requests int64
	Tokens   int64
	Spend    string    `gorm:"type:text"` // JSON object of currency to decimal string
	AsOf     time.Time `gorm:"index"`     // every request in these totals was recorded before it
}

func (ConsoleTotal) TableName() string { return "essera_console_totals" }

// LocalUse counts the requests one subject was admitted for, and the tokens of the requests that
// finished, in one second; the Console's totals do not hold them yet when the second is at or
// after their as_of. Measurement only, never money.
type LocalUse struct {
	Scope    string    `gorm:"primaryKey;type:varchar(16)"`
	Subject  string    `gorm:"primaryKey;type:varchar(400)"`
	Month    string    `gorm:"primaryKey;type:varchar(7)"`
	At       time.Time `gorm:"primaryKey"` // the second
	Requests int64
	Tokens   int64
}

func (LocalUse) TableName() string { return "essera_local_use" }

// ProviderMinute counts the attempts sent to one provider in one minute, for the provider's
// rate limit across every Gateway node.
type ProviderMinute struct {
	Provider string    `gorm:"primaryKey;type:varchar(64)"`
	Minute   time.Time `gorm:"primaryKey"`
	Requests int64
}

func (ProviderMinute) TableName() string { return "essera_provider_minutes" }

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

// PersonUser links the users row the Gateway built for a Console person to that person. A
// users row without one is not a person's: an operator, or the service user.
type PersonUser struct {
	UserID    int    `gorm:"primaryKey;autoIncrement:false"`
	PersonID  string `gorm:"type:varchar(64);uniqueIndex"`
	Role      string `gorm:"type:varchar(16)"` // the person's role, "admin" or "user"; never a Gateway administrator
	UpdatedAt time.Time
}

func (PersonUser) TableName() string { return "essera_people" }

// Key is an API key a person created on the Gateway: its id, never the key. The new-api token
// row stands for it in the web UI. RevokedAt is when the person deleted it on the Gateway;
// PublishedAt is the first time an applied configuration listed it.
type Key struct {
	KeyID       string     `gorm:"primaryKey;type:varchar(40)"`
	TokenID     int        `gorm:"uniqueIndex"`
	UserID      int        `gorm:"index"`
	PersonID    string     `gorm:"type:varchar(64);index"`
	CustomerID  string     `gorm:"type:varchar(64)"`
	ExpiresAt   time.Time  `gorm:"index"`
	CreatedAt   time.Time  `gorm:"index"`
	RevokedAt   *time.Time `gorm:"index"`
	PublishedAt *time.Time
}

func (Key) TableName() string { return "essera_keys" }

// CognitoSession binds a Gateway web session to the Cognito sign-in behind it. The refresh
// token is encrypted with CRYPTO_SECRET and never leaves the server.
type CognitoSession struct {
	SessionID    string `gorm:"primaryKey;type:varchar(64)"`
	UserID       int    `gorm:"index"`
	Sub          string `gorm:"type:varchar(64)"`
	RefreshToken string `gorm:"type:text"`
	UpdatedAt    time.Time
}

func (CognitoSession) TableName() string { return "essera_cognito_sessions" }

// SignInAttempt counts the sign-in attempts of one subject (an IP or an email digest) in one
// window, across every Gateway node.
type SignInAttempt struct {
	Subject  string `gorm:"primaryKey;type:varchar(80)"`
	Started  int64  `gorm:"primaryKey;autoIncrement:false"` // unix seconds, start of the window
	Attempts int64
}

func (SignInAttempt) TableName() string { return "essera_sign_in_attempts" }

// EmbeddingTask is a customer's video or audio embedding task (Marengo on Bedrock): whose it is,
// so only they read it, and its usage event, written to the outbox once when it ends.
type EmbeddingTask struct {
	TaskID        string `gorm:"primaryKey;type:varchar(64)"`
	Model         string `gorm:"type:varchar(200)"`
	Modality      string `gorm:"type:varchar(16)"` // video or audio
	InvocationARN string `gorm:"type:varchar(256)"`
	Region        string `gorm:"type:varchar(32)"`
	OutputURI     string `gorm:"type:varchar(512)"`
	CustomerID    string `gorm:"type:varchar(128);index"`
	EndUserID     string `gorm:"type:varchar(200)"`
	Status        string `gorm:"type:varchar(16);index"` // new-api's task statuses
	FailReason    string `gorm:"type:text"`
	Seconds       int64  // of media Bedrock processed, once it succeeded
	Event         string `gorm:"type:text"` // usage.recorded as at submission, completed when it ends
	CreatedAt     time.Time
	FinishedAt    *time.Time
	ReportedAt    *time.Time `gorm:"index"`
}

func (EmbeddingTask) TableName() string { return "essera_embedding_tasks" }

// Migrate creates or updates the Essera tables in the primary database.
func Migrate() error {
	// The Gateway's own monthly counters, spend included, gave way to the Console's totals.
	if err := model.DB.Migrator().DropTable("essera_usage_counters"); err != nil {
		return err
	}
	return model.DB.AutoMigrate(&ConfigVersion{}, &OutboxEvent{}, &ConsoleTotal{}, &LocalUse{}, &ProviderMinute{}, &RequestStat{}, &ProviderError{},
		&PersonUser{}, &Key{}, &CognitoSession{}, &SignInAttempt{}, &EmbeddingTask{})
}
