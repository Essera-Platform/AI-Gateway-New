package limits

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TotalsDetailType is the event the Console publishes this month's usage totals in, on the
// configuration queue (usage.totals.detail.schema.json in the Console's event contracts).
const TotalsDetailType = "usage.totals"

// Totals is the detail of a usage.totals event: what each subject used this month, as the
// Console rated it, up to as_of.
type Totals struct {
	Environment string          `json:"environment"`
	Month       string          `json:"month"`
	AsOf        time.Time       `json:"as_of"`
	Subjects    []SubjectTotals `json:"subjects"`
}

type SubjectTotals struct {
	Scope    string            `json:"scope"`
	ID       string            `json:"id"`
	Requests int64             `json:"requests"`
	Tokens   int64             `json:"tokens"`
	Spend    map[string]string `json:"spend"`
}

// ApplyTotals keeps the totals a usage.totals event carries. A subject's totals older than
// those already kept are ignored, so events may arrive in any order. What this Gateway counted
// before every kept as_of of the month is then in the Console's totals and is dropped.
func ApplyTotals(body []byte) error {
	var totals Totals
	if err := common.Unmarshal(body, &totals); err != nil {
		return err
	}
	if totals.Environment != settings.Environment() {
		return fmt.Errorf("usage totals for environment %q, this Gateway serves %q", totals.Environment, settings.Environment())
	}
	if totals.Month == "" || totals.AsOf.IsZero() {
		return fmt.Errorf("usage totals without a month or as_of")
	}
	asOf := totals.AsOf.UTC()
	return model.DB.Transaction(func(tx *gorm.DB) error {
		for _, subject := range totals.Subjects {
			spend, err := common.Marshal(subject.Spend)
			if err != nil {
				return err
			}
			row := records.ConsoleTotal{
				Scope: subject.Scope, Subject: subject.ID, Month: totals.Month,
				Requests: subject.Requests, Tokens: subject.Tokens, Spend: string(spend), AsOf: asOf,
			}
			err = tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "scope"}, {Name: "subject"}, {Name: "month"}},
				DoUpdates: clause.AssignmentColumns([]string{"requests", "tokens", "spend", "as_of"}),
				Where:     clause.Where{Exprs: []clause.Expression{gorm.Expr("essera_console_totals.as_of < excluded.as_of")}},
			}).Create(&row).Error
			if err != nil {
				return err
			}
		}
		if err := tx.Where("month < ?", totals.Month).Delete(&records.ConsoleTotal{}).Error; err != nil {
			return err
		}
		if err := tx.Where("month < ?", totals.Month).Delete(&records.LocalUse{}).Error; err != nil {
			return err
		}
		var oldest records.ConsoleTotal
		err := tx.Where("month = ?", totals.Month).Order("as_of").Limit(1).Find(&oldest).Error
		if err != nil || oldest.AsOf.IsZero() {
			return err
		}
		return tx.Where("month = ? AND at < ?", totals.Month, oldest.AsOf.UTC().Truncate(time.Second)).Delete(&records.LocalUse{}).Error
	})
}
