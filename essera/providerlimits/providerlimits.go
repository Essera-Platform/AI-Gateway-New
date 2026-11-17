// Package providerlimits applies the provider settings the Console publishes to each attempt
// the Gateway makes: the rate limit in requests per minute, the number of retries, and the
// time the provider has to start answering.
package providerlimits

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TagPrefix mirrors configsync.TagPrefix; configsync imports packages this one must not.
const TagPrefix = "essera/"

const retryBudgetKey = "essera_retry_budget"

var lastSwept atomic.Int64

// Of is the Console's provider behind a channel; nil for a channel the Console did not project
// or while no configuration is active.
func Of(channelID int) *snapshot.Provider {
	view := snapshot.Current()
	if view == nil {
		return nil
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil {
		return nil
	}
	key, projected := strings.CutPrefix(channel.GetTag(), TagPrefix)
	if !projected {
		return nil
	}
	return view.Provider(key)
}

// Admit counts an attempt against the provider's rate limit and makes the provider's retries
// the request's retry budget. A refusal is final: retrying would only count again.
func Admit(c *gin.Context, channelID int, now time.Time) *types.NewAPIError {
	if !settings.Managed() {
		return nil
	}
	provider := Of(channelID)
	if provider == nil {
		return nil
	}
	c.Set(retryBudgetKey, provider.MaxRetries)
	if provider.RateLimitRPM == nil || *provider.RateLimitRPM <= 0 {
		return nil
	}
	count, err := countAttempt(provider.Key, now)
	if err != nil {
		common.SysError(common.LogText("essera: counting provider attempts: %s", err.Error()))
		return types.NewErrorWithStatusCode(errors.New("The gateway cannot check the provider's rate limit right now."), types.ErrorCode("essera_unavailable"), http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if count <= int64(*provider.RateLimitRPM) {
		return nil
	}
	stats.Denial(now, "provider_rate_limit", http.StatusTooManyRequests)
	return types.NewErrorWithStatusCode(errors.New("The provider's request rate limit is reached; retry in a minute."), types.ErrorCode("essera_provider_rate_limited"), http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
}

// RetryTimes is the request's retry budget: the provider's retries once an attempt was
// admitted, otherwise the gateway-wide setting.
func RetryTimes(c *gin.Context, gatewayWide int) int {
	if budget, ok := c.Get(retryBudgetKey); ok {
		if retries, isInt := budget.(int); isInt {
			return retries
		}
	}
	return gatewayWide
}

// Timeout is how long the provider behind the channel has to start answering; zero when the
// Console sets none.
func Timeout(channelID int) time.Duration {
	if !settings.Managed() {
		return 0
	}
	provider := Of(channelID)
	if provider == nil || provider.TimeoutMs <= 0 {
		return 0
	}
	return time.Duration(provider.TimeoutMs) * time.Millisecond
}

func countAttempt(provider string, now time.Time) (int64, error) {
	minute := now.UTC().Truncate(time.Minute)
	row := records.ProviderMinute{Provider: provider, Minute: minute, Requests: 1}
	err := model.DB.Clauses(
		clause.OnConflict{
			Columns:   []clause.Column{{Name: "provider"}, {Name: "minute"}},
			DoUpdates: clause.Assignments(map[string]any{"requests": gorm.Expr("essera_provider_minutes.requests + 1")}),
		},
		clause.Returning{Columns: []clause.Column{{Name: "requests"}}},
	).Create(&row).Error
	if err != nil {
		return 0, err
	}
	if previous := lastSwept.Swap(minute.Unix()); previous != minute.Unix() {
		model.DB.Where("minute < ?", minute.Add(-time.Hour)).Delete(&records.ProviderMinute{})
	}
	return row.Requests, nil
}
