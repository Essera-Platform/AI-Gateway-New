package configsync

import (
	"context"
	"encoding/json"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
)

// envelope is an EventBridge event; the queue may also carry the detail on its own.
type envelope struct {
	DetailType string          `json:"detail-type"`
	Detail     json.RawMessage `json:"detail"`
}

// decodeMessage returns the event's detail type, "" when the body is a bare detail, and its
// detail.
func decodeMessage(body string) (string, []byte) {
	var wrapped envelope
	if err := common.UnmarshalJsonStr(body, &wrapped); err == nil && len(wrapped.Detail) > 0 && string(wrapped.Detail) != "null" {
		return wrapped.DetailType, wrapped.Detail
	}
	return "", []byte(body)
}

func decodeEvent(body string) (snapshot.Event, error) {
	_, detail := decodeMessage(body)
	var event snapshot.Event
	err := common.Unmarshal(detail, &event)
	return event, err
}

// Consume reads the configuration queue until ctx ends. A message that fails is left on the
// queue, so it is retried and, after the queue's receive limit, reaches its dead-letter queue.
func Consume(ctx context.Context) {
	var queueURL string
	for ctx.Err() == nil {
		if queueURL == "" {
			url, err := awsx.QueueURL(ctx, settings.ConfigQueueName())
			if err != nil {
				common.SysError(common.LogText("essera: configuration queue %s: %s", settings.ConfigQueueName(), err.Error()))
				sleep(ctx, time.Minute)
				continue
			}
			queueURL = url
		}
		messages, err := awsx.Receive(ctx, queueURL, 10)
		if err != nil {
			common.SysError(common.LogText("essera: reading the configuration queue: %s", err.Error()))
			sleep(ctx, 10*time.Second)
			continue
		}
		for _, message := range messages {
			handleMessage(ctx, queueURL, message)
		}
	}
}

func handleMessage(ctx context.Context, queueURL string, message awsx.Message) {
	if detailType, detail := decodeMessage(message.Body); detailType == limits.TotalsDetailType {
		if err := limits.ApplyTotals(detail); err != nil {
			common.SysError(common.LogText("essera: usage totals: %s", err.Error()))
			return
		}
		if err := awsx.Delete(ctx, queueURL, message.ReceiptHandle); err != nil {
			common.SysError(common.LogText("essera: acknowledging usage totals: %s", err.Error()))
		}
		return
	}
	event, err := decodeEvent(message.Body)
	if err != nil {
		common.SysError(common.LogText("essera: unreadable configuration event: %s", err.Error()))
		return
	}
	if err := HandleEvent(ctx, event); err != nil {
		common.SysError(common.LogText("essera: configuration version %d: %s", event.Version, err.Error()))
		return
	}
	if err := awsx.Delete(ctx, queueURL, message.ReceiptHandle); err != nil {
		common.SysError(common.LogText("essera: acknowledging configuration version %d: %s", event.Version, err.Error()))
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
