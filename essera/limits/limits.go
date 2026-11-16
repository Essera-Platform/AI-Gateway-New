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

// Exceeded names the first limit a subject has already reached: "requests", "tokens" or
// "spend"; "" when none is.
func Exceeded(subjects []Subject, month, currency string) (string, error) {
	for _, subject := range subjects {
		limits := subject.Limits
		if limits.MonthlyRequestQuota == nil && limits.MonthlyTokenQuota == nil && limits.MonthlySpendLimit == nil {
			continue
		}
		var used records.Counter
		err := model.DB.Where("scope = ? AND subject = ? AND month = ? AND currency = ?", subject.Scope, subject.ID, month, currency).First(&used).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		switch {
		case limits.MonthlyRequestQuota != nil && used.Requests >= *limits.MonthlyRequestQuota:
			return "requests", nil
		case limits.MonthlyTokenQuota != nil && used.Tokens >= *limits.MonthlyTokenQuota:
			return "tokens", nil
		case limits.MonthlySpendLimit != nil && used.Spend.GreaterThanOrEqual(*limits.MonthlySpendLimit):
			return "spend", nil
		}
	}
	return "", nil
}

// Add counts one served request against every subject.
func Add(subjects []Subject, month, currency string, tokens int64, spend decimal.Decimal) error {
	for _, subject := range subjects {
		row := records.Counter{Scope: subject.Scope, Subject: subject.ID, Month: month, Currency: currency, Requests: 1, Tokens: tokens, Spend: spend}
		err := model.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "scope"}, {Name: "subject"}, {Name: "month"}, {Name: "currency"}},
			DoUpdates: clause.Assignments(map[string]any{
				"requests": gorm.Expr("essera_usage_counters.requests + ?", 1),
				"tokens":   gorm.Expr("essera_usage_counters.tokens + ?", tokens),
				"spend":    gorm.Expr("essera_usage_counters.spend + ?", spend),
			}),
		}).Create(&row).Error
		if err != nil {
			return err
		}
	}
	return nil
}
