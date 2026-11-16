package configsync

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
)

// envelope is an EventBridge event; the queue may also carry the detail on its own.
type envelope struct {
	DetailType string          `json:"detail-type"`
	Detail     *snapshot.Event `json:"detail"`
}

func decodeEvent(body string) (snapshot.Event, error) {
	var wrapped envelope
	if err := common.UnmarshalJsonStr(body, &wrapped); err == nil && wrapped.Detail != nil {
		return *wrapped.Detail, nil
	}
	var event snapshot.Event
	err := common.UnmarshalJsonStr(body, &event)
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
