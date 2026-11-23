// Package availability tells when AWS cannot serve a model: such a failure is not
// retried on a channel that already failed, and a model whose recent calls all
// failed that way is marked temporarily unavailable on this instance for the
// Model Square and the Playground.
package availability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	failuresToMark = 2
	failureWindow  = 10 * time.Minute
	markTTL        = 5 * time.Minute
)

// Unavailable reports whether err is Bedrock's ServiceUnavailableException ("Bedrock
// is unable to process your request"), as the AWS adaptor maps it.
func Unavailable(err *types.NewAPIError) bool {
	return err != nil && err.StatusCode == http.StatusServiceUnavailable && err.GetErrorCode() == "service_unavailable"
}

// UntriedChannelLeft reports whether a retry of this request could reach a channel
// it has not tried yet. With cross-group retry on an auto group another group may
// serve the model, so the answer is true.
func UntriedChannelLeft(c *gin.Context, filters []dto.ChannelFilter) bool {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		if common.GetContextKeyBool(c, constant.ContextKeyTokenCrossGroupRetry) {
			return true
		}
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	var tried []int
	for _, used := range c.GetStringSlice("use_channel") {
		if id, err := strconv.Atoi(used); err == nil {
			tried = append(tried, id)
		}
	}
	return model.HasUntriedChannel(group, common.GetContextKeyString(c, constant.ContextKeyOriginalModel), tried, filters)
}

type modelState struct {
	failures    []time.Time
	markedUntil time.Time
}

// Tracker keeps each model's recent Unavailable failures since its last success.
type Tracker struct {
	mu     sync.Mutex
	models map[string]*modelState
}

func NewTracker() *Tracker {
	return &Tracker{models: map[string]*modelState{}}
}

// Record notes the outcome of one attempt to call modelName. A success clears the
// model; Unavailable failures within failureWindow, failuresToMark of them in a row,
// mark it until markTTL after the last one. Other failures say nothing about
// availability and are ignored.
func (t *Tracker) Record(modelName string, err *types.NewAPIError, now time.Time) {
	if modelName == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil {
		delete(t.models, modelName)
		return
	}
	if !Unavailable(err) {
		return
	}
	state := t.models[modelName]
	if state == nil {
		state = &modelState{}
		t.models[modelName] = state
	}
	recent := state.failures[:0]
	for _, at := range state.failures {
		if now.Sub(at) < failureWindow {
			recent = append(recent, at)
		}
	}
	state.failures = append(recent, now)
	if len(state.failures) >= failuresToMark {
		state.markedUntil = now.Add(markTTL)
	}
}

// TemporarilyUnavailable reports whether modelName is marked at now.
func (t *Tracker) TemporarilyUnavailable(modelName string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.models[modelName]
	return state != nil && now.Before(state.markedUntil)
}

var instance = NewTracker()

// Record notes an attempt's outcome in this instance's tracker.
func Record(modelName string, err *types.NewAPIError) {
	instance.Record(modelName, err, time.Now())
}

// TemporarilyUnavailable reads this instance's tracker.
func TemporarilyUnavailable(modelName string) bool {
	return instance.TemporarilyUnavailable(modelName, time.Now())
}
