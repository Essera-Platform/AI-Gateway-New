// Package embeddingtasks serves the video and audio embedding tasks of a managed gateway
// (TwelveLabs Marengo on Bedrock): it keeps each task Bedrock accepted with its customer, polls
// it until it ends, reports its usage once, and answers GET /v1/embeddings/tasks/:task_id.
package embeddingtasks

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/aws"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gin-gonic/gin"
)

const (
	pollEvery = 10 * time.Second
	pollBatch = 100
	// expiresAfter is how long the output bucket keeps a task's embeddings.
	expiresAfter = 7 * 24 * time.Hour
)

// pollTask and taskOutput reach Bedrock and S3; tests replace them.
var (
	pollTask   = aws.PollEmbeddingTask
	taskOutput = aws.EmbeddingTaskOutput
)

// Task is a task as a client reads it, in new-api's task shape, with its embeddings once done.
type Task struct {
	Object     string                  `json:"object"`
	TaskID     string                  `json:"task_id"`
	Model      string                  `json:"model"`
	Status     string                  `json:"status"`
	FailReason string                  `json:"fail_reason,omitempty"`
	CreatedAt  int64                   `json:"created_at"`
	FinishedAt int64                   `json:"finished_at,omitempty"`
	Data       []aws.EmbeddingTaskItem `json:"data,omitempty"`
	Usage      map[string]int64        `json:"usage,omitempty"`
}

func view(row records.EmbeddingTask) Task {
	task := Task{Object: "embedding.task", TaskID: row.TaskID, Model: row.Model, Status: row.Status, FailReason: row.FailReason, CreatedAt: row.CreatedAt.Unix()}
	if row.FinishedAt != nil {
		task.FinishedAt = row.FinishedAt.Unix()
	}
	if row.Status == model.TaskStatusSuccess {
		task.Usage = map[string]int64{"input_" + row.Modality + "_seconds": row.Seconds}
	}
	return task
}

// Started keeps a task Bedrock accepted with its customer and answers its submission.
func Started(c *gin.Context, info *relaycommon.RelayInfo, task aws.EmbeddingTask) (any, error) {
	event, owner, ok := usage.TaskAccepted(c, info)
	if !ok {
		return nil, errors.New("the task has no Console customer")
	}
	row := records.EmbeddingTask{
		TaskID: task.ID, Model: task.Model, Modality: task.Modality, InvocationARN: task.InvocationARN, Region: task.Region,
		CustomerID: owner.CustomerID, EndUserID: owner.EndUserID, Status: model.TaskStatusSubmitted, Event: event, CreatedAt: time.Now().UTC(),
	}
	if err := model.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	return view(row), nil
}

// Get answers GET /v1/embeddings/tasks/:task_id for the customer who submitted the task, and
// for an end user only their own; an unfinished task is polled first.
func Get(c *gin.Context) {
	value, ok := c.Get(usage.ContextKey)
	if !settings.Managed() || !ok {
		refuse(c, http.StatusNotFound, "task_not_exist", "Task not found.")
		return
	}
	request := value.(*policy.Request)
	var row records.EmbeddingTask
	err := model.DB.Where("task_id = ?", c.Param("task_id")).Limit(1).Find(&row).Error
	if err != nil {
		refuse(c, http.StatusServiceUnavailable, "essera_unavailable", "The gateway cannot read this task right now.")
		return
	}
	if row.TaskID == "" || row.CustomerID != request.Caller.Customer.ID || request.EndUserID != "" && row.EndUserID != request.EndUserID {
		refuse(c, http.StatusNotFound, "task_not_exist", "Task not found.")
		return
	}
	if row.FinishedAt == nil {
		refreshed, err := refresh(c.Request.Context(), row)
		if err != nil {
			common.SysError(common.LogText("essera: polling embedding task %s: %s", row.TaskID, err.Error()))
		} else {
			row = refreshed
		}
	}
	answer := view(row)
	if row.Status == model.TaskStatusSuccess {
		items, err := taskOutput(c.Request.Context(), row.Region, row.OutputURI)
		var missing *types.NoSuchKey
		switch {
		case errors.As(err, &missing) || err != nil && time.Since(*row.FinishedAt) > expiresAfter:
			refuse(c, http.StatusGone, "task_expired", "The task's embeddings are kept for 7 days and have expired.")
			return
		case err != nil:
			common.SysError(common.LogText("essera: reading embedding task %s: %s", row.TaskID, err.Error()))
			refuse(c, http.StatusServiceUnavailable, "essera_unavailable", "The gateway cannot read this task's embeddings right now.")
			return
		}
		answer.Data = items
	}
	c.JSON(http.StatusOK, answer)
}

func refuse(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": "invalid_request_error", "code": code}})
}

// refresh polls an unfinished task and, once it ended, settles it: its state, the seconds
// Bedrock processed, and its usage event.
func refresh(ctx context.Context, row records.EmbeddingTask) (records.EmbeddingTask, error) {
	state, err := pollTask(ctx, aws.EmbeddingTask{ID: row.TaskID, InvocationARN: row.InvocationARN, Region: row.Region})
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
		state, err = aws.EmbeddingTaskState{Done: true, FailReason: "Bedrock has no such invocation"}, nil
	}
	if err != nil {
		return row, err
	}
	if !state.Done {
		if row.Status != model.TaskStatusInProgress {
			row.Status = model.TaskStatusInProgress
			err = model.DB.Model(&records.EmbeddingTask{}).Where("task_id = ? AND finished_at IS NULL", row.TaskID).Update("status", row.Status).Error
		}
		return row, err
	}
	now := time.Now().UTC()
	row.FinishedAt, row.OutputURI, row.FailReason, row.Status = &now, state.OutputURI, state.FailReason, model.TaskStatusFailure
	if state.FailReason == "" {
		items, err := taskOutput(ctx, row.Region, state.OutputURI)
		if err != nil {
			return row, err
		}
		row.Status, row.Seconds = model.TaskStatusSuccess, aws.MediaSeconds(items)
	}
	settled := model.DB.Model(&records.EmbeddingTask{}).Where("task_id = ? AND finished_at IS NULL", row.TaskID).Updates(map[string]any{
		"status": row.Status, "fail_reason": row.FailReason, "output_uri": row.OutputURI, "seconds": row.Seconds, "finished_at": now,
	})
	if settled.Error != nil {
		return row, settled.Error
	}
	if settled.RowsAffected == 1 {
		report(row)
	}
	return row, nil
}

// report queues the usage event of an ended task; a task whose event could not be queued is
// reported again by the next poll.
func report(row records.EmbeddingTask) {
	outcome := usage.TaskOutcome{FinishedAt: *row.FinishedAt, Modality: row.Modality, Seconds: row.Seconds, Failed: row.Status == model.TaskStatusFailure}
	if err := usage.TaskEnded(row.Event, outcome); err != nil {
		common.SysError(common.LogText("essera: queueing the usage of embedding task %s: %s", row.TaskID, err.Error()))
		return
	}
	if err := model.DB.Model(&records.EmbeddingTask{}).Where("task_id = ?", row.TaskID).Update("reported_at", time.Now().UTC()).Error; err != nil {
		common.SysError(common.LogText("essera: marking embedding task %s reported: %s", row.TaskID, err.Error()))
	}
}

// Poll settles the unfinished tasks and reports the ended ones not yet reported, once.
func Poll(ctx context.Context) {
	var open []records.EmbeddingTask
	if err := model.DB.Where("finished_at IS NULL").Order("created_at").Limit(pollBatch).Find(&open).Error; err != nil {
		common.SysError(common.LogText("essera: reading embedding tasks: %s", err.Error()))
		return
	}
	for _, row := range open {
		if _, err := refresh(ctx, row); err != nil {
			common.SysError(common.LogText("essera: polling embedding task %s: %s", row.TaskID, err.Error()))
		}
	}
	var unreported []records.EmbeddingTask
	if err := model.DB.Where("finished_at IS NOT NULL AND reported_at IS NULL").Limit(pollBatch).Find(&unreported).Error; err != nil {
		return
	}
	for _, row := range unreported {
		report(row)
	}
}

// Run polls every 10 seconds until ctx ends; the master node runs it.
func Run(ctx context.Context) {
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Poll(ctx)
		}
	}
}
