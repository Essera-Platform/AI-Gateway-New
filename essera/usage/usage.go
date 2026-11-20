// Package usage reports every request a customer's credential had served to the Console, as
// the usage.recorded event of the Console's contract (F8, ADR-010), through an outbox so a
// queue outage loses nothing. The same moment counts its tokens for the monthly limits and in
// the telemetry. It measures; it never prices: the Console rates every event.
package usage

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/metering"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

// ContextKey holds the *policy.Request of an authenticated Essera request.
const ContextKey = "essera_request"

// SkipContextKey marks a request that must not be reported, such as a connection test.
const SkipContextKey = "essera_skip_usage"

// Event is usage.recorded, with the optional fields the Console stores in the record's payload.
type Event struct {
	EventID              string    `json:"event_id"`
	TS                   time.Time `json:"ts"`
	TenantID             *string   `json:"tenant_id,omitempty"`
	CustomerID           string    `json:"customer_id"`
	EndUserID            *string   `json:"end_user_id,omitempty"`
	CredentialRef        string    `json:"credential_ref,omitempty"` // none in the playground
	RequestID            string    `json:"request_id"`
	Provider             string    `json:"provider"`
	Model                string    `json:"model"`
	Modality             string    `json:"modality"`
	InputUnits           int64     `json:"input_units"`
	CacheReadUnits       int64     `json:"cache_read_units"`
	CacheWriteUnits      int64     `json:"cache_write_units"`
	CacheWrite1hUnits    int64     `json:"cache_write_1h_units"`
	OutputUnits          int64     `json:"output_units"`
	GeneratedUnits       int64     `json:"generated_units"`
	InputImageUnits      int64     `json:"input_image_units"`
	Status               string    `json:"status"`
	ProviderRef          *string   `json:"provider_ref,omitempty"`
	CorrelationID        *string   `json:"correlation_id,omitempty"`
	ConfigurationVersion int64     `json:"configuration_version"`
	LatencyMs            int64     `json:"latency_ms"`
	FirstTokenMs         *int64    `json:"first_token_ms,omitempty"`
	HTTPStatus           int       `json:"http_status"`
	ErrorCategory        *string   `json:"error_category,omitempty"`
	Retries              int       `json:"retries"`
}

// Finished reports a relayed request once its outcome is final. A request that never reached
// a provider is counted as refused, not reported.
func Finished(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	value, ok := c.Get(ContextKey)
	if !ok || c.GetBool(SkipContextKey) {
		return
	}
	request := value.(*policy.Request)
	now := time.Now()
	if info == nil || info.ChannelMeta == nil || info.ChannelId == 0 {
		if apiErr == nil || apiErr.GetErrorCode() != types.ErrorCode("essera_quota_exceeded") {
			stats.Denial(now, "model", statusOf(apiErr))
		}
		release(request)
		return
	}
	event := build(c, request, info, apiErr, now)
	stats.Request(now, event.Provider, event.Model, event.Status, event.HTTPStatus, time.Duration(event.LatencyMs)*time.Millisecond, event.Retries)
	if apiErr != nil {
		stats.ProviderError(now, event.Provider, event.Model, event.HTTPStatus, apiErr.Error())
	}
	if err := enqueue(event); err != nil {
		common.SysError(common.LogText("essera: queueing usage event %s: %s", event.EventID, err.Error()))
	}
	if err := limits.Add(request.Subjects, request.Month, now, event.tokens()); err != nil {
		common.SysError(common.LogText("essera: counting usage of %s: %s", request.Caller.CredentialRef, err.Error()))
	}
}

func build(c *gin.Context, request *policy.Request, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, now time.Time) Event {
	caller := request.Caller
	requestID := c.GetString(common.RequestIdKey)
	event := Event{
		EventID:              "gw-" + requestID,
		TS:                   info.StartTime.UTC(),
		CustomerID:           caller.Customer.ID,
		CredentialRef:        caller.CredentialRef,
		RequestID:            requestID,
		Provider:             providerOf(info.ChannelId),
		Model:                info.OriginModelName,
		Modality:             modalityOf(info.RelayMode),
		Status:               outcomeOf(apiErr),
		ConfigurationVersion: caller.View.Version,
		LatencyMs:            now.Sub(info.StartTime).Milliseconds(),
		HTTPStatus:           statusOf(apiErr),
		Retries:              info.RetryIndex,
	}
	if caller.Partner != nil {
		event.TenantID = &caller.Partner.ID
	}
	if request.EndUserID != "" {
		event.EndUserID = &request.EndUserID
	}
	if upstream := c.GetString(common.UpstreamRequestIdKey); upstream != "" {
		event.ProviderRef = &upstream
	}
	if correlation := c.GetHeader("X-Essera-Correlation-Id"); correlation != "" {
		event.CorrelationID = &correlation
	}
	if !info.FirstResponseTime.IsZero() && info.FirstResponseTime.After(info.StartTime) {
		first := info.FirstResponseTime.Sub(info.StartTime).Milliseconds()
		event.FirstTokenMs = &first
	}
	if apiErr != nil {
		category := string(apiErr.GetErrorCode())
		event.ErrorCategory = &category
	}
	if units, ok := metering.Recorded(c); ok {
		event.InputUnits, event.OutputUnits = units.Input, units.Output
		event.CacheReadUnits, event.CacheWriteUnits, event.CacheWrite1hUnits = units.CacheRead, units.CacheWrite, units.CacheWrite1h
		event.InputImageUnits = units.InputImages
	} else if consumed, ok := common.GetContextKey(c, constant.ContextKeyConsumeLog); ok {
		if params, ok := consumed.(*model.RecordConsumeLogParams); ok {
			event.InputUnits = int64(params.PromptTokens)
			event.OutputUnits = int64(params.CompletionTokens)
		}
	}
	if event.Status == "succeeded" {
		measureGenerated(&event, info)
	}
	return event
}

// tokens is every token of the request, cached or not, as the monthly token limit counts them.
func (e Event) tokens() int64 {
	return e.InputUnits + e.CacheReadUnits + e.CacheWriteUnits + e.CacheWrite1hUnits + e.OutputUnits
}

// unknownProvider names a channel that cannot be looked up, so its event still reaches the
// Console, which requires a provider and flags the request as unpriced.
const unknownProvider = "unknown"

func providerOf(channelID int) string {
	channel, err := model.CacheGetChannel(channelID)
	if err != nil {
		return unknownProvider
	}
	key := strings.TrimPrefix(channel.GetTag(), configsync.TagPrefix)
	if key == "" {
		return unknownProvider
	}
	return key
}

func modalityOf(mode int) string {
	switch mode {
	case relayconstant.RelayModeEmbeddings, relayconstant.RelayModeRerank:
		return "embedding"
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		return "image"
	case relayconstant.RelayModeAudioSpeech, relayconstant.RelayModeAudioTranscription, relayconstant.RelayModeAudioTranslation:
		return "audio"
	}
	return "text"
}

// audioTokensPerMinute is how new-api measures audio: input audio for transcription and
// translation, output audio for speech, at 1000 tokens a minute of sound.
const audioTokensPerMinute = 1000

// measureGenerated turns new-api's counts into the units the Console prices.
//   - Images: the images actually settled, not the n asked for. new-api writes one prompt token
//     when the provider reports no usage; that is no token at all.
//   - Audio: minutes of sound, rounded up, from new-api's per-minute tokens. They are not tokens,
//     so the token units are cleared.
func measureGenerated(event *Event, info *relaycommon.RelayInfo) {
	switch event.Modality {
	case "image":
		event.GeneratedUnits = imagesSettled(info)
		if event.InputUnits <= 1 && event.OutputUnits == 0 {
			event.InputUnits = 0
		}
	case "audio":
		measured := event.OutputUnits
		if info.RelayMode == relayconstant.RelayModeAudioTranscription || info.RelayMode == relayconstant.RelayModeAudioTranslation {
			measured = event.InputUnits
		}
		event.GeneratedUnits = (measured + audioTokensPerMinute - 1) / audioTokensPerMinute
		event.InputUnits, event.OutputUnits = 0, 0
		event.CacheReadUnits, event.CacheWriteUnits, event.CacheWrite1hUnits = 0, 0, 0
	}
}

func imagesSettled(info *relaycommon.RelayInfo) int64 {
	if info.BillingImageCount != nil {
		return int64(*info.BillingImageCount)
	}
	if request, ok := info.Request.(*dto.ImageRequest); ok && request.N != nil {
		return int64(*request.N)
	}
	return 1
}

func statusOf(apiErr *types.NewAPIError) int {
	if apiErr == nil {
		return http.StatusOK
	}
	if apiErr.StatusCode == 0 {
		return http.StatusInternalServerError
	}
	return apiErr.StatusCode
}

func outcomeOf(apiErr *types.NewAPIError) string {
	switch {
	case apiErr == nil:
		return "succeeded"
	case apiErr.StatusCode == http.StatusTooManyRequests:
		return "rate_limited"
	case apiErr.StatusCode == http.StatusGatewayTimeout || strings.Contains(strings.ToLower(string(apiErr.GetErrorCode())), "timeout"):
		return "timeout"
	}
	return "failed"
}

func enqueue(event Event) error {
	body, err := common.Marshal(event)
	if err != nil {
		return err
	}
	row := records.OutboxEvent{EventID: event.EventID, Body: string(body), CreatedAt: time.Now().UTC()}
	result := model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("an event with this id is already queued")
	}
	return nil
}

// Reserve counts a request a customer sent to relay against the request quotas, before its
// first attempt; past a quota it is refused. A model listing, or a request new-api refuses
// before relay, never reaches here and is not counted.
func Reserve(c *gin.Context) *types.NewAPIError {
	value, ok := c.Get(ContextKey)
	if !ok {
		return nil
	}
	request := value.(*policy.Request)
	if request.Reserved {
		return nil
	}
	at := time.Now()
	exceeded, err := limits.Reserve(request.Subjects, request.Month, at)
	if err != nil {
		common.SysError(common.LogText("essera: reserving a request of %s: %s", request.Caller.CredentialRef, err.Error()))
		return types.NewErrorWithStatusCode(errors.New("The gateway cannot check this account's limits right now."), types.ErrorCode("essera_unavailable"), http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if exceeded != "" {
		stats.Denial(time.Now(), "quota", http.StatusTooManyRequests)
		return types.NewErrorWithStatusCode(errors.New("This account has reached its monthly "+exceeded+" limit."), types.ErrorCode("essera_quota_exceeded"), http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	}
	request.Reserved, request.ReservedAt = true, at
	return nil
}

// release gives back the reservation of a request that reached no provider: the Console has
// no record of it, so it does not count.
func release(request *policy.Request) {
	if !request.Reserved {
		return
	}
	if err := limits.Release(request.Subjects, request.Month, request.ReservedAt); err != nil {
		common.SysError(common.LogText("essera: releasing a request of %s: %s", request.Caller.CredentialRef, err.Error()))
	}
	request.Reserved = false
}
