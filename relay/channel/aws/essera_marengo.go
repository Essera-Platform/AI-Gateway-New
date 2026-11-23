// Essera fork: TwelveLabs Marengo Embed 3.0. Texts and images embed on /v1/embeddings through
// InvokeModel, one call each, priced per text request and per image. Video and audio embed as a
// task on /v1/embeddings/tasks through StartAsyncInvoke, priced per second of media processed:
// Bedrock writes the embeddings to s3://$BEDROCK_OUTPUT_BUCKET/embedding-tasks/<task>/, and the
// Essera integration polls the invocation, reads that output and reports the task once it ends.
package aws

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
)

const (
	// EmbeddingTaskPath submits a video or audio embedding task.
	EmbeddingTaskPath = "/v1/embeddings/tasks"
	// OutputBucketEnv names the bucket, in the provider's region, Bedrock writes asynchronous
	// invocations to; the task role needs s3:PutObject and s3:GetObject on it.
	OutputBucketEnv = "BEDROCK_OUTPUT_BUCKET"
	// marengoDimensions is the one vector length Marengo Embed 3.0 makes.
	marengoDimensions = 512
	// maxMarengoMediaSeconds is the longest video or audio Marengo takes: 4 hours.
	maxMarengoMediaSeconds = 4 * 60 * 60
	embeddingTaskObject    = "output.json"
	embeddingTaskIDKey     = "essera_embedding_task_id"
	asyncCallTimeout       = 30 * time.Second
)

// EmbeddingTask is a Marengo invocation Bedrock accepted.
type EmbeddingTask struct {
	ID            string
	Model         string
	Modality      string // video or audio
	InvocationARN string
	Region        string
}

// EmbeddingTaskStarted keeps a task Bedrock accepted and returns the answer to its submission.
// The Essera integration sets it; without it embedding tasks are not served.
var EmbeddingTaskStarted func(c *gin.Context, info *relaycommon.RelayInfo, task EmbeddingTask) (any, error)

// asyncInvoker is the part of the Bedrock Runtime client an embedding task uses.
type asyncInvoker interface {
	StartAsyncInvoke(ctx context.Context, params *bedrockruntime.StartAsyncInvokeInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.StartAsyncInvokeOutput, error)
	GetAsyncInvoke(ctx context.Context, params *bedrockruntime.GetAsyncInvokeInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.GetAsyncInvokeOutput, error)
}

// newAsyncInvoker and readTaskOutput call AWS with the task role: a Bedrock API key cannot have
// Bedrock write to S3, and the bucket is the Gateway's own. Tests replace them.
var (
	newAsyncInvoker = func(ctx context.Context, region string) (asyncInvoker, error) {
		cfg, err := awsx.Config(ctx)
		if err != nil {
			return nil, err
		}
		return bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
			o.Region = region
			o.BaseEndpoint = nil
		}), nil
	}
	readTaskOutput = func(ctx context.Context, region, bucket, key string) ([]byte, error) {
		cfg, err := awsx.Config(ctx)
		if err != nil {
			return nil, err
		}
		out, err := s3.NewFromConfig(cfg, func(o *s3.Options) { o.Region = region }).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if err != nil {
			return nil, err
		}
		defer out.Body.Close()
		return io.ReadAll(out.Body)
	}
)

func isEmbeddingTaskPath(path string) bool {
	path, _, _ = strings.Cut(path, "?")
	return strings.TrimSuffix(path, "/") == EmbeddingTaskPath
}

// mediaDataURL splits a base64 data URL: its media type ("video/mp4") and data.
func mediaDataURL(input string) (mediaType, data string, ok bool) {
	rest, found := strings.CutPrefix(input, "data:")
	if !found {
		return "", "", false
	}
	mediaType, data, found = strings.Cut(rest, ";base64,")
	return mediaType, data, found && data != ""
}

func marengoMediaSource(data string) map[string]any {
	return map[string]any{"mediaSource": map[string]any{"base64String": data}}
}

// marengoEmbeddingPlan makes one InvokeModel call per input: an image data URL embeds the
// image, anything else is text.
func marengoEmbeddingPlan(modelID string, inputs []string, request dto.EmbeddingRequest) (*embeddingPlan, error) {
	if request.Dimensions != nil && *request.Dimensions != marengoDimensions {
		return nil, newBadRequest("model %s embeds at %d dimensions only", modelID, marengoDimensions)
	}
	plan := &embeddingPlan{}
	for i, input := range inputs {
		mediaType, data, isMedia := mediaDataURL(input)
		var body map[string]any
		switch {
		case isMedia && strings.HasPrefix(mediaType, "image/"):
			body = map[string]any{"inputType": "image", "image": marengoMediaSource(data)}
		case isMedia:
			return nil, newBadRequest("model %s embeds video and audio as a task: POST %s", modelID, EmbeddingTaskPath)
		default:
			body = map[string]any{"inputType": "text", "text": map[string]any{"inputText": input}}
		}
		raw, err := common.Marshal(body)
		if err != nil {
			return nil, err
		}
		plan.Calls = append(plan.Calls, embeddingCall{Inputs: []int{i}, Body: raw, Image: isMedia})
	}
	return plan, nil
}

// marengoVectors reads Marengo's answer: data holds one embedding, as a list or an object.
func marengoVectors(body []byte) ([][]float64, error) {
	var native struct {
		Data any `json:"data"`
	}
	if err := common.Unmarshal(body, &native); err != nil {
		return nil, err
	}
	items, isList := native.Data.([]any)
	if !isList {
		items = []any{native.Data}
	}
	vectors := make([][]float64, 0, len(items))
	for _, item := range items {
		raw, err := common.Marshal(item)
		if err != nil {
			return nil, err
		}
		var embedded struct {
			Embedding []float64 `json:"embedding"`
		}
		if err := common.Unmarshal(raw, &embedded); err != nil {
			return nil, err
		}
		vectors = append(vectors, embedded.Embedding)
	}
	return vectors, nil
}

// textsPricedApart counts the text requests of the plan Bedrock prices per request: Marengo's.
func textsPricedApart(family embeddingFamily, plan *embeddingPlan) int64 {
	if plan == nil || family != embeddingMarengo {
		return 0
	}
	var texts int64
	for _, call := range plan.Calls {
		if !call.Image {
			texts += int64(len(call.Inputs))
		}
	}
	return texts
}

// embeddingTaskRequest is what a task submission adds to an embedding request: Marengo's
// options for video and audio.
type embeddingTaskRequest struct {
	StartSec        *float64       `json:"start_sec,omitempty"`
	EndSec          *float64       `json:"end_sec,omitempty"`
	EmbeddingOption []string       `json:"embedding_option,omitempty"`
	EmbeddingScope  []string       `json:"embedding_scope,omitempty"`
	EmbeddingType   []string       `json:"embedding_type,omitempty"`
	Segmentation    map[string]any `json:"segmentation,omitempty"`
}

// embeddingTaskPlan is the request the task route forwards to DoRequest.
type embeddingTaskPlan struct {
	Modality   string         `json:"modality"`
	ModelInput map[string]any `json:"modelInput"`
}

// convertEmbeddingTaskRequest builds Marengo's asynchronous input for one video or audio given
// as a base64 data URL. The seconds billed are read from Bedrock's output, not from here.
func convertEmbeddingTaskRequest(modelID string, request dto.EmbeddingRequest, options embeddingTaskRequest) (*embeddingTaskPlan, error) {
	if embeddingFamilyOf(modelID) != embeddingMarengo {
		return nil, newBadRequest("model %s does not embed video or audio as a task", modelID)
	}
	if EmbeddingTaskStarted == nil {
		return nil, types.NewErrorWithStatusCode(errors.New("embedding tasks are served on a gateway the Essera Operations Console manages"), types.ErrorCodeInvalidRequest, http.StatusNotFound, types.ErrOptionWithSkipRetry())
	}
	inputs := request.ParseInput()
	if len(inputs) != 1 {
		return nil, newBadRequest("input must be one video or audio as a base64 data URL")
	}
	mediaType, data, ok := mediaDataURL(inputs[0])
	modality, _, _ := strings.Cut(mediaType, "/")
	if !ok || (modality != "video" && modality != "audio") {
		return nil, newBadRequest("input must be one video or audio as a base64 data URL; embed text and images on /v1/embeddings")
	}
	if request.Dimensions != nil && *request.Dimensions != marengoDimensions {
		return nil, newBadRequest("model %s embeds at %d dimensions only", modelID, marengoDimensions)
	}
	media := marengoMediaSource(data)
	for name, value := range map[string]*float64{"start_sec": options.StartSec, "end_sec": options.EndSec} {
		if value != nil && (*value < 0 || *value > maxMarengoMediaSeconds || math.IsNaN(*value)) {
			return nil, newBadRequest("%s must be from 0 to %d", name, maxMarengoMediaSeconds)
		}
	}
	if options.StartSec != nil && options.EndSec != nil && *options.EndSec <= *options.StartSec {
		return nil, newBadRequest("end_sec must be after start_sec")
	}
	if options.StartSec != nil {
		media["startSec"] = *options.StartSec
	}
	if options.EndSec != nil {
		media["endSec"] = *options.EndSec
	}
	if len(options.EmbeddingOption) > 0 {
		media["embeddingOption"] = options.EmbeddingOption
	}
	if len(options.EmbeddingScope) > 0 {
		media["embeddingScope"] = options.EmbeddingScope
	}
	if len(options.EmbeddingType) > 0 {
		media["embeddingType"] = options.EmbeddingType
	}
	if options.Segmentation != nil {
		media["segmentation"] = options.Segmentation
	}
	return &embeddingTaskPlan{Modality: modality, ModelInput: map[string]any{"inputType": modality, modality: media}}, nil
}

// asyncModelID is the model an asynchronous invocation names: Bedrock starts one on the base
// model only, never through an inference profile.
func asyncModelID(modelID string) string {
	geography, base, found := strings.Cut(modelID, ".")
	if found && strings.Contains(base, ".") && (geography == "us" || geography == "eu" || geography == "apac" || geography == "global") {
		return base
	}
	return modelID
}

// startEmbeddingTask starts the invocation. The task id is the request's, so a retry of the
// same request is the same invocation (Bedrock's client request token).
func (a *Adaptor) startEmbeddingTask(c *gin.Context, info *relaycommon.RelayInfo, body []byte) error {
	var plan embeddingTaskPlan
	if err := common.Unmarshal(body, &plan); err != nil || plan.ModelInput == nil {
		return types.NewErrorWithStatusCode(errors.New("invalid AWS Bedrock embedding task"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	creds, err := parseBedrockCredentials(info.ApiKey)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	bucket := strings.TrimSpace(common.GetEnvOrDefaultString(OutputBucketEnv, ""))
	if bucket == "" {
		return types.NewError(fmt.Errorf("%s is not set", OutputBucketEnv), types.ErrorCodeChannelAwsClientError, types.ErrOptionWithSkipRetry())
	}
	taskID := c.GetString(embeddingTaskIDKey)
	if taskID == "" {
		taskID = "embtask-" + common.GetRandomString(24)
		c.Set(embeddingTaskIDKey, taskID)
	}
	a.modelID = asyncModelID(getAwsModelID(info.UpstreamModelName))
	ctx, cancel := context.WithTimeout(c.Request.Context(), asyncCallTimeout)
	defer cancel()
	invoker, err := newAsyncInvoker(ctx, creds.region)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	out, err := invoker.StartAsyncInvoke(ctx, &bedrockruntime.StartAsyncInvokeInput{
		ClientRequestToken: aws.String(taskID),
		ModelId:            aws.String(a.modelID),
		ModelInput:         document.NewLazyDocument(plan.ModelInput),
		OutputDataConfig: &bedrockruntimeTypes.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig{
			Value: bedrockruntimeTypes.AsyncInvokeS3OutputDataConfig{S3Uri: aws.String("s3://" + bucket + "/embedding-tasks/" + taskID + "/")},
		},
	})
	if err != nil {
		return newAwsInvokeError(c.Request.Context(), err, "StartAsyncInvoke")
	}
	a.embeddingTask = &EmbeddingTask{
		ID:            taskID,
		Model:         info.OriginModelName,
		Modality:      plan.Modality,
		InvocationARN: aws.ToString(out.InvocationArn),
		Region:        creds.region,
	}
	return nil
}

// EmbeddingTaskState is where an invocation stands.
type EmbeddingTaskState struct {
	Done       bool
	FailReason string // set when Bedrock failed it
	OutputURI  string // where Bedrock wrote its output, once done
}

// PollEmbeddingTask reads the invocation's state.
func PollEmbeddingTask(ctx context.Context, task EmbeddingTask) (EmbeddingTaskState, error) {
	ctx, cancel := context.WithTimeout(ctx, asyncCallTimeout)
	defer cancel()
	invoker, err := newAsyncInvoker(ctx, task.Region)
	if err != nil {
		return EmbeddingTaskState{}, err
	}
	out, err := invoker.GetAsyncInvoke(ctx, &bedrockruntime.GetAsyncInvokeInput{InvocationArn: aws.String(task.InvocationARN)})
	if err != nil {
		return EmbeddingTaskState{}, err
	}
	switch out.Status {
	case bedrockruntimeTypes.AsyncInvokeStatusCompleted:
		state := EmbeddingTaskState{Done: true}
		if s3Output, ok := out.OutputDataConfig.(*bedrockruntimeTypes.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig); ok {
			state.OutputURI = aws.ToString(s3Output.Value.S3Uri)
		}
		return state, nil
	case bedrockruntimeTypes.AsyncInvokeStatusFailed:
		reason := aws.ToString(out.FailureMessage)
		if reason == "" {
			reason = "Bedrock could not embed the media"
		}
		return EmbeddingTaskState{Done: true, FailReason: reason}, nil
	}
	return EmbeddingTaskState{}, nil
}

// EmbeddingTaskItem is one embedding of a task's media: a clip of it, or the whole asset.
type EmbeddingTaskItem struct {
	Object          string    `json:"object"`
	Index           int       `json:"index"`
	Embedding       []float64 `json:"embedding"`
	EmbeddingOption string    `json:"embedding_option,omitempty"`
	EmbeddingScope  string    `json:"embedding_scope,omitempty"`
	StartSec        float64   `json:"start_sec"`
	EndSec          float64   `json:"end_sec"`
}

// EmbeddingTaskOutput reads the embeddings Bedrock wrote for a completed task.
func EmbeddingTaskOutput(ctx context.Context, region, outputURI string) ([]EmbeddingTaskItem, error) {
	location, found := strings.CutPrefix(outputURI, "s3://")
	bucket, prefix, _ := strings.Cut(location, "/")
	if !found || bucket == "" {
		return nil, errors.New("the task has no output location")
	}
	ctx, cancel := context.WithTimeout(ctx, asyncCallTimeout)
	defer cancel()
	body, err := readTaskOutput(ctx, region, bucket, strings.TrimSuffix(prefix, "/")+"/"+embeddingTaskObject)
	if err != nil {
		return nil, err
	}
	var output struct {
		Data []struct {
			Embedding       []float64 `json:"embedding"`
			EmbeddingOption string    `json:"embeddingOption"`
			EmbeddingScope  string    `json:"embeddingScope"`
			StartSec        float64   `json:"startSec"`
			EndSec          float64   `json:"endSec"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &output); err != nil {
		return nil, fmt.Errorf("decode the task's output: %w", err)
	}
	items := make([]EmbeddingTaskItem, len(output.Data))
	for i, item := range output.Data {
		items[i] = EmbeddingTaskItem{Object: "embedding", Index: i, Embedding: item.Embedding, EmbeddingOption: item.EmbeddingOption, EmbeddingScope: item.EmbeddingScope, StartSec: item.StartSec, EndSec: item.EndSec}
	}
	return items, nil
}

// MediaSeconds is the length of media the embeddings cover, from the earliest start to the
// latest end, in whole seconds rounded up (to the millisecond first, so a float's noise past
// the second is no extra second), at most Marengo's 4 hours.
func MediaSeconds(items []EmbeddingTaskItem) int64 {
	if len(items) == 0 {
		return 0
	}
	start, end := math.Inf(1), math.Inf(-1)
	for _, item := range items {
		start, end = min(start, item.StartSec), max(end, item.EndSec)
	}
	span := math.Round((end-start)*1000) / 1000
	if math.IsNaN(span) || span <= 0 {
		return 0
	}
	return int64(min(math.Ceil(span), maxMarengoMediaSeconds))
}
