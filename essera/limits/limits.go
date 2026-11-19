// Package limits checks partners', customers', credentials' and end users' monthly request,
// token and spend limits (M05-C03) against what the Console published they used this month
// (usage.totals), plus the requests and tokens this Gateway counted since. The Console is the
// only place that rates usage: the Gateway computes no money, and spend is the Console's figure.
package limits

import (
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
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

// Used is what a subject used this month: the Console's totals and, for requests and tokens,
// what this Gateway counted from their as_of on.
type Used struct {
	Requests int64
	Tokens   int64
	Spend    map[string]decimal.Decimal
}

// Exceeded names the first limit a subject has already reached: "requests", "tokens" or
// "spend"; "" when none is.
func Exceeded(subjects []Subject, month, currency string) (string, error) {
	var watermark *time.Time
	for _, subject := range subjects {
		limits := subject.Limits
		if limits.MonthlyRequestQuota == nil && limits.MonthlyTokenQuota == nil && limits.MonthlySpendLimit == nil {
			continue
		}
		if watermark == nil {
			published, err := monthWatermark(month)
			if err != nil {
				return "", err
			}
			watermark = &published
		}
		used, err := usedSince(subject, month, *watermark)
		if err != nil {
			return "", err
		}
		switch {
		case limits.MonthlyRequestQuota != nil && used.Requests >= *limits.MonthlyRequestQuota:
			return "requests", nil
		case limits.MonthlyTokenQuota != nil && used.Tokens >= *limits.MonthlyTokenQuota:
			return "tokens", nil
		case limits.MonthlySpendLimit != nil && used.Spend[currency].GreaterThanOrEqual(*limits.MonthlySpendLimit):
			return "spend", nil
		}
	}
	return "", nil
}

var unpublished sync.Map // month → true once logged

// monthWatermark is the newest as_of the Console published for the month. A subject the
// Console did not list then had used nothing; zero when no totals arrived for the month yet,
// and the Gateway enforces with zero spend and its own counts until they do.
func monthWatermark(month string) (time.Time, error) {
	var newest records.ConsoleTotal
	if err := model.DB.Where("month = ?", month).Order("as_of DESC").Limit(1).Find(&newest).Error; err != nil {
		return time.Time{}, err
	}
	if newest.AsOf.IsZero() {
		if _, logged := unpublished.LoadOrStore(month, true); !logged {
			common.SysLog(common.LogText("essera: no usage totals from the Console for %s yet; limits use zero spend and this Gateway's own counts", month))
		}
		return time.Time{}, nil
	}
	return newest.AsOf.UTC(), nil
}

func usedSince(subject Subject, month string, watermark time.Time) (Used, error) {
	used := Used{Spend: map[string]decimal.Decimal{}}
	var total records.ConsoleTotal
	err := model.DB.Where("scope = ? AND subject = ? AND month = ?", subject.Scope, subject.ID, month).First(&total).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
	case err != nil:
		return used, err
	default:
		used.Requests, used.Tokens, watermark = total.Requests, total.Tokens, total.AsOf.UTC()
		var spend map[string]string
		if err := common.UnmarshalJsonStr(total.Spend, &spend); err != nil {
			return used, err
		}
		for currency, amount := range spend {
			value, err := decimal.NewFromString(amount)
			if err != nil {
				return used, err
			}
			used.Spend[currency] = value
		}
	}
	var local struct{ Requests, Tokens int64 }
	err = model.DB.Model(&records.LocalUse{}).
		Select("COALESCE(SUM(requests), 0) AS requests, COALESCE(SUM(tokens), 0) AS tokens").
		Where("scope = ? AND subject = ? AND month = ? AND at >= ?", subject.Scope, subject.ID, month, watermark).
		Scan(&local).Error
	used.Requests += local.Requests
	used.Tokens += local.Tokens
	return used, err
}

// Reserve counts the request against every subject before it is served, so concurrent
// requests cannot all pass the last unit of a request quota. Past a quota the request is not
// counted and "requests" is returned.
func Reserve(subjects []Subject, month string, at time.Time) (string, error) {
	var reserved []Subject
	var watermark *time.Time
	for _, subject := range subjects {
		if err := count(subject, month, at, "requests", 1); err != nil {
			return "", errors.Join(err, Release(reserved, month, at))
		}
		reserved = append(reserved, subject)
		quota := subject.Limits.MonthlyRequestQuota
		if quota == nil {
			continue
		}
		if watermark == nil {
			published, err := monthWatermark(month)
			if err != nil {
				return "", errors.Join(err, Release(reserved, month, at))
			}
			watermark = &published
		}
		used, err := usedSince(subject, month, *watermark)
		if err != nil {
			return "", errors.Join(err, Release(reserved, month, at))
		}
		if used.Requests > *quota {
			return "requests", Release(reserved, month, at)
		}
	}
	return "", nil
}

// Release gives back the reservation made at `at` of every subject.
func Release(subjects []Subject, month string, at time.Time) error {
	for _, subject := range subjects {
		err := model.DB.Model(&records.LocalUse{}).
			Where("scope = ? AND subject = ? AND month = ? AND at = ?", subject.Scope, subject.ID, month, second(at)).
			Update("requests", gorm.Expr("requests - 1")).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// Add counts the tokens of a request that finished at `at`, whatever its outcome, as the
// Console counts every request.
func Add(subjects []Subject, month string, at time.Time, tokens int64) error {
	if tokens == 0 {
		return nil
	}
	for _, subject := range subjects {
		if err := count(subject, month, at, "tokens", tokens); err != nil {
			return err
		}
	}
	return nil
}

// second is the bucket a moment counts in. A bucket counts only when it starts at or after the
// Console's as_of, so the second as_of falls in is left to the Console: a request in it may be
// counted late, never twice.
func second(at time.Time) time.Time {
	return at.UTC().Truncate(time.Second)
}

func count(subject Subject, month string, at time.Time, column string, amount int64) error {
	row := records.LocalUse{Scope: subject.Scope, Subject: subject.ID, Month: month, At: second(at)}
	if column == "requests" {
		row.Requests = amount
	} else {
		row.Tokens = amount
	}
	return model.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "subject"}, {Name: "month"}, {Name: "at"}},
		DoUpdates: clause.Assignments(map[string]any{column: gorm.Expr("essera_local_use."+column+" + ?", amount)}),
	}).Create(&row).Error
}
