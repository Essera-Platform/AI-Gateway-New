package usage

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// A customer's asynchronous task is one request to the Console: its usage event is written
// once, when the task ends, with the caller kept from its submission.

// TaskSubmission ends a customer's task submission. A task the provider accepted is kept with
// its caller until it ends; a submission the provider refused is reported as failed, with no
// units; one that never reached the provider gives its reservation back.
func TaskSubmission(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, taskErr *taskdto.TaskError) {
	value, ok := c.Get(ContextKey)
	if !ok || c.GetBool(SkipContextKey) {
		return
	}
	request := value.(*policy.Request)
	now := time.Now()
	if task == nil {
		if taskErr == nil || info == nil || info.ChannelMeta == nil || info.ChannelId == 0 || taskErr.LocalError {
			if taskErr != nil && taskErr.StatusCode != http.StatusTooManyRequests {
				stats.Denial(now, "invalid_request", taskErr.StatusCode)
			}
			release(request)
			return
		}
		Finished(c, info, types.NewErrorWithStatusCode(errors.New(taskErr.Message), types.ErrorCode(taskErr.Code), taskErr.StatusCode))
		return
	}
	record := taskRecord(c, request, info, task)
	if err := model.DB.Create(&record).Error; err != nil {
		common.SysError(common.LogText("essera: keeping the caller of task %s: %s", task.TaskID, err.Error()))
		return
	}
	stats.Request(now, record.Provider, record.Model, "succeeded", http.StatusOK, now.Sub(info.StartTime), info.RetryIndex)
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		TaskFinished(task)
	}
}

func taskRecord(c *gin.Context, request *policy.Request, info *relaycommon.RelayInfo, task *model.Task) records.Task {
	caller := request.Caller
	subjects := make([]limits.Subject, 0, len(request.Subjects))
	for _, subject := range request.Subjects {
		subjects = append(subjects, limits.Subject{Scope: subject.Scope, ID: subject.ID})
	}
	encoded, _ := common.Marshal(subjects)
	record := records.Task{
		TaskID:               task.TaskID,
		RequestID:            c.GetString(common.RequestIdKey),
		CustomerID:           caller.Customer.ID,
		CredentialRef:        caller.ClientID,
		EndUserID:            request.EndUserID,
		CorrelationID:        c.GetHeader("X-Essera-Correlation-Id"),
		Provider:             providerOf(info.ChannelId),
		Model:                info.OriginModelName,
		ProviderRef:          task.GetUpstreamTaskID(),
		ConfigurationVersion: caller.View.Version,
		Currency:             request.Currency,
		Month:                request.Month,
		Subjects:             string(encoded),
		Seconds:              int64(info.PriceData.OtherRatios()["seconds"]),
		Retries:              info.RetryIndex,
		SubmittedAt:          info.StartTime.UTC(),
	}
	if caller.Partner != nil {
		record.TenantID = caller.Partner.ID
	}
	return record
}

// TaskFinished writes the usage event of a customer's task that reached its final state: the
// video's seconds when it succeeded, nothing when it failed. Each task is reported once.
func TaskFinished(task *model.Task) {
	if task == nil || (task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure) {
		return
	}
	var record records.Task
	if err := model.DB.Where("task_id = ? AND reported_at IS NULL", task.TaskID).Limit(1).Find(&record).Error; err != nil || record.TaskID == "" {
		return
	}
	now := time.Now().UTC()
	claimed := model.DB.Model(&records.Task{}).Where("task_id = ? AND reported_at IS NULL", task.TaskID).Update("reported_at", now)
	if claimed.Error != nil || claimed.RowsAffected != 1 {
		return
	}
	event, spend := taskEvent(record, task, now)
	if err := enqueue(event); err != nil {
		common.SysError(common.LogText("essera: queueing usage event %s: %s", event.EventID, err.Error()))
	}
	var subjects []limits.Subject
	_ = common.UnmarshalJsonStr(record.Subjects, &subjects)
	if err := limits.Add(subjects, record.Month, record.Currency, 0, spend); err != nil {
		common.SysError(common.LogText("essera: counting usage of %s: %s", record.CredentialRef, err.Error()))
	}
}

func taskEvent(record records.Task, task *model.Task, now time.Time) (Event, decimal.Decimal) {
	event := Event{
		EventID:              "gw-" + record.RequestID,
		TS:                   record.SubmittedAt,
		CustomerID:           record.CustomerID,
		CredentialRef:        record.CredentialRef,
		RequestID:            record.RequestID,
		Provider:             record.Provider,
		Model:                record.Model,
		Modality:             "video",
		Status:               "succeeded",
		ConfigurationVersion: record.ConfigurationVersion,
		LatencyMs:            now.Sub(record.SubmittedAt).Milliseconds(),
		HTTPStatus:           http.StatusOK,
		Retries:              record.Retries,
	}
	if task.FinishTime > 0 {
		event.LatencyMs = time.Unix(task.FinishTime, 0).Sub(record.SubmittedAt).Milliseconds()
	}
	if record.RequestID == "" {
		event.EventID = "gw-" + task.TaskID
	}
	if record.TenantID != "" {
		event.TenantID = &record.TenantID
	}
	if record.EndUserID != "" {
		event.EndUserID = &record.EndUserID
	}
	if record.CorrelationID != "" {
		event.CorrelationID = &record.CorrelationID
	}
	if record.ProviderRef != "" {
		event.ProviderRef = &record.ProviderRef
	}
	if task.Status == model.TaskStatusSuccess {
		event.GeneratedUnits = record.Seconds
	} else {
		category := "task_failed"
		event.Status, event.HTTPStatus, event.ErrorCategory = "failed", http.StatusBadGateway, &category
	}
	return event, priceIn(snapshot.Current(), record.CustomerID, record.TenantID, record.EndUserID, record.Currency, &event)
}

// TaskCustomer is the customer who submitted the task; found is false for a task no customer
// submitted.
func TaskCustomer(taskID string) (customerID string, found bool, err error) {
	var record records.Task
	if err := model.DB.Select("task_id", "customer_id").Where("task_id = ?", taskID).Limit(1).Find(&record).Error; err != nil {
		return "", false, err
	}
	return record.CustomerID, record.TaskID != "", nil
}
