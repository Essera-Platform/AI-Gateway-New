// Package limits keeps what partners, customers, credentials and end users used this month and
// checks it against their monthly request, token and spend limits (M05-C03).
package limits

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Subject is one level whose usage counts against its own limits.
type Subject struct {
	Scope  string // partner, customer, credential, end_user
	ID     string
	Limits snapshot.Entitlements
}

// Month is the calendar month in the platform's timezone, as the Console counts it.
func Month(view *snapshot.View, now time.Time) string {
	return now.In(view.Location()).Format("2006-01")
}

// Counts holds requests and tokens without a currency, so a partner whose customers use
// different currencies still has one request and one token count; spend is kept by currency.
const Counts = ""

// Exceeded names the first limit a subject has already reached: "requests", "tokens" or
// "spend"; "" when none is.
func Exceeded(subjects []Subject, month, currency string) (string, error) {
	for _, subject := range subjects {
		limits := subject.Limits
		if limits.MonthlyRequestQuota == nil && limits.MonthlyTokenQuota == nil && limits.MonthlySpendLimit == nil {
			continue
		}
		counts, err := used(subject, month, Counts)
		if err != nil {
			return "", err
		}
		spent, err := used(subject, month, currency)
		if err != nil {
			return "", err
		}
		switch {
		case limits.MonthlyRequestQuota != nil && counts.Requests >= *limits.MonthlyRequestQuota:
			return "requests", nil
		case limits.MonthlyTokenQuota != nil && counts.Tokens >= *limits.MonthlyTokenQuota:
			return "tokens", nil
		case limits.MonthlySpendLimit != nil && spent.Spend.GreaterThanOrEqual(*limits.MonthlySpendLimit):
			return "spend", nil
		}
	}
	return "", nil
}

func used(subject Subject, month, currency string) (records.Counter, error) {
	var row records.Counter
	err := model.DB.Where("scope = ? AND subject = ? AND month = ? AND currency = ?", subject.Scope, subject.ID, month, currency).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return records.Counter{Spend: decimal.Zero}, nil
	}
	return row, err
}

// Reserve counts the request against every subject before it is served, so concurrent
// requests cannot all pass the last unit of a request quota. Past a quota the request is not
// counted and "requests" is returned.
func Reserve(subjects []Subject, month string) (string, error) {
	var reserved []Subject
	for _, subject := range subjects {
		row := records.Counter{Scope: subject.Scope, Subject: subject.ID, Month: month, Currency: Counts, Requests: 1, Spend: decimal.Zero}
		err := model.DB.Clauses(
			clause.OnConflict{
				Columns:   []clause.Column{{Name: "scope"}, {Name: "subject"}, {Name: "month"}, {Name: "currency"}},
				DoUpdates: clause.Assignments(map[string]any{"requests": gorm.Expr("essera_usage_counters.requests + 1")}),
			},
			clause.Returning{Columns: []clause.Column{{Name: "requests"}}},
		).Create(&row).Error
		if err != nil {
			return "", errors.Join(err, release(reserved, month))
		}
		reserved = append(reserved, subject)
		if quota := subject.Limits.MonthlyRequestQuota; quota != nil && row.Requests > *quota {
			return "requests", release(reserved, month)
		}
	}
	return "", nil
}

// Release gives back a reservation of every subject.
func Release(subjects []Subject, month string) error { return release(subjects, month) }

func release(subjects []Subject, month string) error {
	for _, subject := range subjects {
		err := model.DB.Model(&records.Counter{}).
			Where("scope = ? AND subject = ? AND month = ? AND currency = ?", subject.Scope, subject.ID, month, Counts).
			Update("requests", gorm.Expr("requests - 1")).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// Add counts what a request used, whatever its outcome, as the Console counts every request:
// its tokens, and its billable amount in the account's currency.
func Add(subjects []Subject, month, currency string, tokens int64, spend decimal.Decimal) error {
	for _, subject := range subjects {
		if err := increment(subject, month, Counts, "tokens", tokens); err != nil {
			return err
		}
		if spend.IsZero() {
			continue
		}
		if err := increment(subject, month, currency, "spend", spend); err != nil {
			return err
		}
	}
	return nil
}

func increment(subject Subject, month, currency, column string, amount any) error {
	row := records.Counter{Scope: subject.Scope, Subject: subject.ID, Month: month, Currency: currency, Spend: decimal.Zero}
	switch value := amount.(type) {
	case int64:
		row.Tokens = value
	case decimal.Decimal:
		row.Spend = value
	}
	return model.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "subject"}, {Name: "month"}, {Name: "currency"}},
		DoUpdates: clause.Assignments(map[string]any{column: gorm.Expr("essera_usage_counters."+column+" + ?", amount)}),
	}).Create(&row).Error
}
