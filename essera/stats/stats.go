// Package stats records what every node served and refused, in five-minute buckets, for the
// telemetry the Console shows (M07-O02..O04, M02-O07). Requests are counted in memory and
// written every 30 seconds, so a request costs no database write of its own.
package stats

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LatencyEdges are the upper bounds, in milliseconds, of the latency histogram buckets; the
// last bucket holds everything slower.
var LatencyEdges = []int64{100, 250, 500, 1000, 2000, 4000, 8000, 16000, 32000, 60000}

const bucketSeconds = 300

type key struct {
	bucket     int64
	provider   string
	model      string
	outcome    string
	httpStatus int
}

type tally struct {
	requests   int64
	retries    int64
	latencySum int64
	histogram  []int64
}

var (
	mu      sync.Mutex
	pending = map[key]*tally{}
	errs    []records.ProviderError
)

func bucketOf(at time.Time) int64 { return at.Unix() - at.Unix()%bucketSeconds }

// Request counts a request that reached a provider.
func Request(at time.Time, provider, model, outcome string, httpStatus int, latency time.Duration, retries int) {
	k := key{bucketOf(at), provider, model, outcome, httpStatus}
	mu.Lock()
	defer mu.Unlock()
	t := pending[k]
	if t == nil {
		t = &tally{histogram: make([]int64, len(LatencyEdges)+1)}
		pending[k] = t
	}
	ms := latency.Milliseconds()
	t.requests++
	t.retries += int64(retries)
	t.latencySum += ms
	slot := len(LatencyEdges)
	for i, edge := range LatencyEdges {
		if ms <= edge {
			slot = i
			break
		}
	}
	t.histogram[slot]++
}

// Denial counts a request refused before routing, by reason.
func Denial(at time.Time, reason string, httpStatus int) {
	Request(at, "", "", "denied:"+reason, httpStatus, 0, 0)
}

// ProviderError keeps a provider's error message for the recent-errors list.
func ProviderError(at time.Time, provider, model string, httpStatus int, message string) {
	if len(message) > 500 {
		message = message[:500]
	}
	mu.Lock()
	defer mu.Unlock()
	errs = append(errs, records.ProviderError{At: at.UTC(), Provider: provider, Model: model, HTTPStatus: httpStatus, Message: message})
}

// Flush writes what was counted since the last flush.
func Flush() error {
	mu.Lock()
	taken, takenErrs := pending, errs
	pending, errs = map[key]*tally{}, nil
	mu.Unlock()
	for k, t := range taken {
		histogram, err := common.Marshal(t.histogram)
		if err != nil {
			return err
		}
		row := records.RequestStat{Bucket: k.bucket, Provider: k.provider, Model: k.model, Outcome: k.outcome, HTTPStatus: k.httpStatus,
			Requests: t.requests, Retries: t.retries, LatencySum: t.latencySum, Histogram: string(histogram)}
		if err := upsert(row, t.histogram); err != nil {
			return err
		}
	}
	if len(takenErrs) > 0 {
		return model.DB.Create(&takenErrs).Error
	}
	return nil
}

// upsert adds a tally to its row; the histogram is merged in Go under a row lock.
func upsert(row records.RequestStat, histogram []int64) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var existing records.RequestStat
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("bucket = ? AND provider = ? AND model = ? AND outcome = ? AND http_status = ?", row.Bucket, row.Provider, row.Model, row.Outcome, row.HTTPStatus).
			First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		var merged []int64
		_ = common.UnmarshalJsonStr(existing.Histogram, &merged)
		for i := range histogram {
			if i < len(merged) {
				merged[i] += histogram[i]
			} else {
				merged = append(merged, histogram[i])
			}
		}
		encoded, err := common.Marshal(merged)
		if err != nil {
			return err
		}
		return tx.Model(&existing).Where("bucket = ? AND provider = ? AND model = ? AND outcome = ? AND http_status = ?", row.Bucket, row.Provider, row.Model, row.Outcome, row.HTTPStatus).
			Updates(map[string]any{
				"requests":    existing.Requests + row.Requests,
				"retries":     existing.Retries + row.Retries,
				"latency_sum": existing.LatencySum + row.LatencySum,
				"histogram":   string(encoded),
			}).Error
	})
}

// Run flushes every 30 seconds and prunes what telemetry no longer reads: buckets older than
// eight days and errors older than one.
func Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = Flush()
			return
		case <-ticker.C:
			if err := Flush(); err != nil {
				common.SysError(common.LogText("essera: writing request statistics: %s", err.Error()))
			}
			now := time.Now()
			model.DB.Where("bucket < ?", now.Add(-8*24*time.Hour).Unix()).Delete(&records.RequestStat{})
			model.DB.Where("at < ?", now.Add(-24*time.Hour)).Delete(&records.ProviderError{})
		}
	}
}
