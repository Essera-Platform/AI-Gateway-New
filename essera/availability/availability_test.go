package availability

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

const fable = "anthropic.claude-fable-5"

var (
	start       = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	unavailable = types.WithOpenAIError(types.OpenAIError{Message: "AWS Bedrock: Bedrock is unable to process your request.", Type: "server_error", Code: "service_unavailable"}, http.StatusServiceUnavailable)
	notReady    = types.WithOpenAIError(types.OpenAIError{Message: "AWS Bedrock: Model is not ready", Type: "server_error", Code: "model_not_ready"}, http.StatusServiceUnavailable)
	badRequest  = types.NewErrorWithStatusCode(errors.New("messages: text field is blank"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
)

type attempt struct {
	after time.Duration
	err   *types.NewAPIError
}

func TestTrackerMarksAModelWhoseRecentCallsAllFailedUnavailable(t *testing.T) {
	for _, test := range []struct {
		name     string
		attempts []attempt
		readAt   time.Duration
		want     bool
	}{
		{name: "one failure is not enough", attempts: []attempt{{0, unavailable}}, readAt: time.Second, want: false},
		{name: "two failures within ten minutes mark it", attempts: []attempt{{0, unavailable}, {9 * time.Minute, unavailable}}, readAt: 9*time.Minute + time.Second, want: true},
		{name: "failures further apart do not", attempts: []attempt{{0, unavailable}, {11 * time.Minute, unavailable}}, readAt: 11*time.Minute + time.Second, want: false},
		{name: "a success clears the mark", attempts: []attempt{{0, unavailable}, {time.Minute, unavailable}, {2 * time.Minute, nil}}, readAt: 2*time.Minute + time.Second, want: false},
		{name: "a success between failures restarts the count", attempts: []attempt{{0, unavailable}, {time.Minute, nil}, {2 * time.Minute, unavailable}}, readAt: 2*time.Minute + time.Second, want: false},
		{name: "the mark expires five minutes after the last failure", attempts: []attempt{{0, unavailable}, {time.Minute, unavailable}}, readAt: 6 * time.Minute, want: false},
		{name: "other failures say nothing about availability", attempts: []attempt{{0, unavailable}, {time.Minute, badRequest}, {2 * time.Minute, unavailable}}, readAt: 2*time.Minute + time.Second, want: true},
		{name: "a model that is not ready is not marked", attempts: []attempt{{0, notReady}, {time.Minute, notReady}}, readAt: time.Minute + time.Second, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracker := NewTracker()
			for _, a := range test.attempts {
				tracker.Record(fable, a.err, start.Add(a.after))
			}
			assert.Equal(t, test.want, tracker.TemporarilyUnavailable(fable, start.Add(test.readAt)))
			assert.False(t, tracker.TemporarilyUnavailable("amazon.nova-lite-v1:0", start.Add(test.readAt)), "other models stay available")
		})
	}
}
