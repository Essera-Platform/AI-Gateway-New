package bridge

import (
	"context"
	"time"

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

// TestProvider calls the provider the Console names with its configured key (M02-C07).
func TestProvider(ctx context.Context, providerKey string) TestResult {
	var channel model.Channel
	if err := model.DB.Where("tag = ?", configsync.TagPrefix+providerKey).First(&channel).Error; err != nil {
		return TestResult{Message: "The Gateway has no such provider; its configuration may not have arrived yet."}
	}
	if RunChannelTest == nil {
		return TestResult{Message: "The Gateway cannot run tests."}
	}
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return RunChannelTest(testCtx, &channel)
}
