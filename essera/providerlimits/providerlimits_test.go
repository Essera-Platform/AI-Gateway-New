package providerlimits

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useProvider projects the sample's OpenAI provider, with the given settings, into channel 1.
func useProvider(t *testing.T, rpm *int, retries, timeoutMs int) {
	t.Setenv("ESSERA_MANAGED", "true")
	recordstest.UseDatabase(t, &model.Channel{})
	tag := TagPrefix + "openai"
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "openai", Tag: &tag, Key: "k"}).Error)
	sample := snapshottest.Sample()
	openai := &sample.Providers.Providers[0]
	openai.RateLimitRPM, openai.MaxRetries, openai.TimeoutMs = rpm, retries, timeoutMs
	snapshot.Activate(snapshot.NewView(7, sample))
	t.Cleanup(func() { snapshot.Activate(nil) })
}

func attempt(t *testing.T, at time.Time) (*gin.Context, int) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if refused := Admit(c, 1, at); refused != nil {
		return c, refused.StatusCode
	}
	return c, http.StatusOK
}

func TestTheProvidersRateLimitRefusesTheAttemptsOverItInAMinute(t *testing.T) {
	limit := 2
	useProvider(t, &limit, 1, 0)
	minute := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

	_, first := attempt(t, minute)
	_, second := attempt(t, minute.Add(20*time.Second))
	_, third := attempt(t, minute.Add(40*time.Second))
	_, nextMinute := attempt(t, minute.Add(time.Minute))

	assert.Equal(t, []int{200, 200, 429, 200}, []int{first, second, third, nextMinute})
}

func TestTheProvidersRetriesBecomeTheRequestsBudget(t *testing.T) {
	useProvider(t, nil, 3, 0)
	c, status := attempt(t, time.Now())

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, 3, RetryTimes(c, 1))
	untouched, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Equal(t, 1, RetryTimes(untouched, 1), "before an admitted attempt the gateway-wide value holds")
}

func TestTheProvidersTimeoutAppliesOnlyWhenManaged(t *testing.T) {
	useProvider(t, nil, 0, 1500)
	assert.Equal(t, 1500*time.Millisecond, Timeout(1))
	assert.Zero(t, Timeout(2), "a channel the Console did not project")

	t.Setenv("ESSERA_MANAGED", "false")
	assert.Zero(t, Timeout(1))
}
