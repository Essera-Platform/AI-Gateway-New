package usage

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/model"
)

// Send delivers queued events to the usage queue until ctx ends. Delivery is at least once:
// the Console records each event once by its id.
func Send(ctx context.Context) {
	var queueURL string
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if queueURL == "" {
			url, err := awsx.QueueURL(ctx, settings.UsageQueueName())
			if err != nil {
				common.SysError(common.LogText("essera: usage queue %s: %s", settings.UsageQueueName(), err.Error()))
				continue
			}
			queueURL = url
		}
		if err := sendPending(ctx, queueURL); err != nil {
			common.SysError(common.LogText("essera: sending usage events: %s", err.Error()))
		}
	}
}

// maxAttempts is how often SQS may refuse an event before it stops being offered; it stays in
// the outbox, counted in the backlog the Console sees, for someone to look at.
const maxAttempts = 20

func sendPending(ctx context.Context, queueURL string) error {
	var pending []records.OutboxEvent
	// The least tried first, so events SQS keeps refusing never hold back the ones behind them.
	if err := model.DB.Where("attempts < ?", maxAttempts).Order("attempts, id").Limit(100).Find(&pending).Error; err != nil {
		return err
	}
	for start := 0; start < len(pending); start += 10 {
		batch := pending[start:min(start+10, len(pending))]
		entries := make([]awsx.BatchEntry, 0, len(batch))
		for _, row := range batch {
			entries = append(entries, awsx.BatchEntry{ID: strconv.FormatInt(row.ID, 10), Body: row.Body})
		}
		failed, err := awsx.SendBatch(ctx, queueURL, entries)
		if err != nil {
			return err
		}
		refused := map[string]bool{}
		for _, id := range failed {
			refused[id] = true
		}
		var sent []int64
		for _, row := range batch {
			if refused[strconv.FormatInt(row.ID, 10)] {
				model.DB.Model(&row).Update("attempts", row.Attempts+1)
				continue
			}
			sent = append(sent, row.ID)
		}
		if len(sent) > 0 {
			if err := model.DB.Where("id IN ?", sent).Delete(&records.OutboxEvent{}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// Backlog is how many events wait and how old the oldest is.
func Backlog() (count int64, oldest *time.Time) {
	model.DB.Model(&records.OutboxEvent{}).Count(&count)
	var row records.OutboxEvent
	if count > 0 && model.DB.Order("id").First(&row).Error == nil {
		oldest = &row.CreatedAt
	}
	return count, oldest
}
