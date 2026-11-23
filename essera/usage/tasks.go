package usage

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

// A customer's asynchronous task is one request to the Console: its submission counts against
// the request quotas like any request, and its one usage event is written when it ends, with
// the units the provider processed.

const taskAcceptedKey = "essera_task_accepted"

// TaskOwner is whose task it is: only they read it.
type TaskOwner struct {
	CustomerID string
	EndUserID  string
}

// TaskAccepted takes over the report of a submission the provider accepted: it returns the
// request's usage event as it stands, to be completed when the task ends, and Finished then
// writes none. ok is false for a request with no Console customer.
func TaskAccepted(c *gin.Context, info *relaycommon.RelayInfo) (event string, owner TaskOwner, ok bool) {
	value, found := c.Get(ContextKey)
	if !found || c.GetBool(SkipContextKey) {
		return "", TaskOwner{}, false
	}
	request := value.(*policy.Request)
	body, err := common.Marshal(build(c, request, info, nil, time.Now()))
	if err != nil {
		return "", TaskOwner{}, false
	}
	c.Set(taskAcceptedKey, true)
	return string(body), TaskOwner{CustomerID: request.Caller.Customer.ID, EndUserID: request.EndUserID}, true
}

// TaskOutcome is how a task ended.
type TaskOutcome struct {
	FinishedAt time.Time
	Modality   string // video or audio
	Seconds    int64  // of media the provider processed; none when it failed
	Failed     bool
}

// TaskEnded queues the usage event of a task that ended: the seconds of video or audio it
// processed, or failed with no units. Queuing it again is a no-op.
func TaskEnded(pending string, outcome TaskOutcome) error {
	var event Event
	if err := common.UnmarshalJsonStr(pending, &event); err != nil {
		return err
	}
	event.LatencyMs = max(outcome.FinishedAt.Sub(event.TS).Milliseconds(), 0)
	event.InputUnits, event.OutputUnits = 0, 0
	switch {
	case outcome.Failed:
		category := "task_failed"
		event.Status, event.HTTPStatus, event.ErrorCategory = "failed", http.StatusBadGateway, &category
	case outcome.Modality == "video":
		event.InputVideoSeconds = outcome.Seconds
	case outcome.Modality == "audio":
		event.InputAudioSeconds = outcome.Seconds
	default:
		return errors.New("a task's modality must be video or audio")
	}
	body, err := common.Marshal(event)
	if err != nil {
		return err
	}
	row := records.OutboxEvent{EventID: event.EventID, Body: string(body), CreatedAt: time.Now().UTC()}
	return model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}
