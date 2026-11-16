package bridge

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/model"
)

// TestResult is how one provider answered the Gateway's test request.
type TestResult struct {
	Succeeded bool
	Latency   time.Duration
	Message   string
}

// RunChannelTest calls a provider through its channel with a minimal request of the service
// user; the controller package sets it (new-api's channel test lives there).
var RunChannelTest func(ctx context.Context, channel *model.Channel) TestResult

type testRequest struct {
	ID          string    `json:"id"`
	ProviderKey string    `json:"provider_key"`
	RequestedAt time.Time `json:"requested_at"`
}

type testAnswer struct {
	ID          string    `json:"id"`
	ProviderKey string    `json:"provider_key"`
	Succeeded   bool      `json:"succeeded"`
	LatencyMs   *int64    `json:"latency_ms"`
	Message     *string   `json:"message"`
	CompletedAt time.Time `json:"completed_at"`
}

// AnswerTests runs every pending connection test the Console asked for.
func AnswerTests(ctx context.Context) error {
	bucket := configsync.Bucket()
	if bucket == "" || RunChannelTest == nil {
		return nil
	}
	keys, err := awsx.ListKeys(ctx, bucket, objectKey("tests/"))
	if err != nil {
		return err
	}
	for _, key := range keys {
		if strings.HasSuffix(key, ".request.json") {
			answer(ctx, bucket, key)
		}
	}
	return nil
}

func answer(ctx context.Context, bucket, key string) {
	defer func() { _ = awsx.DeleteObject(ctx, bucket, key) }()
	data, found, err := awsx.GetObject(ctx, bucket, key)
	if err != nil || !found {
		return
	}
	var request testRequest
	if err := common.Unmarshal(data, &request); err != nil || time.Since(request.RequestedAt) > time.Minute {
		return
	}
	result := test(ctx, request.ProviderKey)
	reply := testAnswer{ID: request.ID, ProviderKey: request.ProviderKey, Succeeded: result.Succeeded, CompletedAt: time.Now().UTC()}
	if result.Latency > 0 {
		latency := result.Latency.Milliseconds()
		reply.LatencyMs = &latency
	}
	if result.Message != "" {
		reply.Message = &result.Message
	}
	body, err := common.Marshal(reply)
	if err != nil {
		return
	}
	resultKey := strings.TrimSuffix(key, ".request.json") + ".result.json"
	if err := awsx.PutJSON(ctx, bucket, resultKey, body); err != nil {
		common.SysError(common.LogText("essera: answering connection test %s: %s", request.ID, err.Error()))
	}
}

func test(ctx context.Context, providerKey string) TestResult {
	var channel model.Channel
	if err := model.DB.Where("tag = ?", configsync.TagPrefix+providerKey).First(&channel).Error; err != nil {
		return TestResult{Message: "The Gateway has no such provider; its configuration may not have arrived yet."}
	}
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return RunChannelTest(testCtx, &channel)
}
