package bridge

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// taskLimits are the CPU and memory the ECS task may use, from the metadata ECS serves every
// container at ECS_CONTAINER_METADATA_URI_V4; nil outside ECS. They do not change while the task
// runs, so they are read once.
type taskLimits struct {
	CPU    *float64 `json:"CPU"`
	Memory *float64 `json:"Memory"`
}

var (
	limitsOnce sync.Once
	limits     taskLimits
)

func readTaskLimits() taskLimits {
	limitsOnce.Do(func() {
		base := os.Getenv("ECS_CONTAINER_METADATA_URI_V4")
		if base == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/task", nil)
		if err != nil {
			return
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return
		}
		defer response.Body.Close()
		var task struct {
			Limits taskLimits `json:"Limits"`
		}
		if common.DecodeJson(response.Body, &task) == nil {
			limits = task.Limits
		}
	})
	return limits
}
