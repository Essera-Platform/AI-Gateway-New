package bridge

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/usage"
)

// Run publishes the state every 15 seconds and the telemetry every minute, and answers
// connection tests every 2 seconds, until ctx ends. Only the master node runs it.
func Run(ctx context.Context) {
	tests := time.NewTicker(2 * time.Second)
	state := time.NewTicker(15 * time.Second)
	telemetry := time.NewTicker(time.Minute)
	defer tests.Stop()
	defer state.Stop()
	defer telemetry.Stop()
	publishState(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tests.C:
			if err := AnswerTests(ctx); err != nil {
				common.SysError(common.LogText("essera: connection tests: %s", err.Error()))
			}
		case <-state.C:
			publishState(ctx)
		case <-telemetry.C:
			current, err := CurrentTelemetry(time.Now())
			if err == nil {
				err = PublishTelemetry(ctx, current)
			}
			if err != nil {
				common.SysError(common.LogText("essera: publishing telemetry: %s", err.Error()))
			}
		}
	}
}

func publishState(ctx context.Context) {
	backlog, _ := usage.Backlog()
	if err := PublishState(ctx, CurrentState(time.Now(), backlog)); err != nil {
		common.SysError(common.LogText("essera: publishing state: %s", err.Error()))
	}
}
