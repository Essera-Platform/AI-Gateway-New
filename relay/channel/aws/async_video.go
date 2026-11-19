// Essera fork: Bedrock's asynchronous video generation (StartAsyncInvoke, GetAsyncInvoke) as a
// native task adaptor. Bedrock writes the video to s3://$BEDROCK_OUTPUT_BUCKET/<task>/, and a
// finished task's result is a presigned GET URL signed afresh on every read.
package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/gin-gonic/gin"
)

// AsyncOutputBucketEnv names the bucket Bedrock writes generated videos to. It must be in the
// region of the provider, and the task role needs s3:PutObject and s3:GetObject on it.
const AsyncOutputBucketEnv = "BEDROCK_OUTPUT_BUCKET"

// asyncResultURLTTL is how long a presigned video URL stays valid.
const asyncResultURLTTL = 15 * time.Minute

// asyncVideoObject is the file Bedrock writes under the invocation's output prefix.
const asyncVideoObject = "output.mp4"

const asyncCallTimeout = 30 * time.Second

// asyncBaseEndpoint replaces the Bedrock runtime endpoint; tests point it at a fixture server.
var asyncBaseEndpoint = ""

// asyncTaskRoleCredentials are the task role's credentials (essera/awsx). Bedrock API keys
// cannot write the output to S3 or sign S3 URLs, so a channel holding one invokes and presigns
// with the task role; a channel holding an access key and secret uses them.
var asyncTaskRoleCredentials = awsx.Credentials

type asyncVideoFamily string

const (
	asyncNovaReel asyncVideoFamily = "nova-reel"
	asyncLumaRay  asyncVideoFamily = "luma-ray"
)

func asyncVideoFamilyOf(modelID string) (asyncVideoFamily, bool) {
	switch {
	case strings.Contains(modelID, "amazon.nova-reel-"):
		return asyncNovaReel, true
	case strings.Contains(modelID, "luma.ray-"):
		return asyncLumaRay, true
	}
	return "", false
}

// IsAsyncVideoModel tells whether Bedrock serves the model by asynchronous invocation.
func IsAsyncVideoModel(modelID string) bool {
	_, ok := asyncVideoFamilyOf(modelID)
	return ok
}

// asyncInvocation is what the task keeps of GetAsyncInvoke: its Data and the poll body.
type asyncInvocation struct {
	InvocationArn  string     `json:"invocationArn"`
	ModelArn       string     `json:"modelArn,omitempty"`
	Status         string     `json:"status"`
	FailureMessage string     `json:"failureMessage,omitempty"`
	SubmitTime     *time.Time `json:"submitTime,omitempty"`
	EndTime        *time.Time `json:"endTime,omitempty"`
	OutputS3URI    string     `json:"outputS3Uri,omitempty"`
	Region         string     `json:"region,omitempty"`
}

// asyncStartRequest is the StartAsyncInvoke body, in Bedrock's own shape so the egress request
// plugins check what is sent.
type asyncStartRequest struct {
	ClientRequestToken string         `json:"clientRequestToken"`
	ModelID            string         `json:"modelId"`
	ModelInput         map[string]any `json:"modelInput"`
	OutputDataConfig   struct {
		S3OutputDataConfig struct {
			S3URI string `json:"s3Uri"`
		} `json:"s3OutputDataConfig"`
	} `json:"outputDataConfig"`
}

// AsyncVideoAdaptor serves Bedrock video models on the task routes.
type AsyncVideoAdaptor struct {
	taskcommon.BaseBilling
	modelInput map[string]any
	seconds    int
}

func (a *AsyncVideoAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *AsyncVideoAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextToVideo); taskErr != nil {
		return taskErr
	}
	request, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	modelID := info.UpstreamModelName
	if modelID == "" {
		modelID = request.Model
	}
	family, ok := asyncVideoFamilyOf(modelID)
	if !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model %s is not a Bedrock video model", modelID), "invalid_model", http.StatusBadRequest)
	}
	var input map[string]any
	var seconds int
	switch family {
	case asyncNovaReel:
		input, seconds, err = novaReelInput(modelID, request)
	case asyncLumaRay:
		input, seconds, err = lumaRayInput(request)
	}
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if request.HasImage() {
		info.Action = constant.TaskActionImageToVideo
	}
	a.modelInput, a.seconds = input, seconds
	return nil
}

func taskSeconds(request relaycommon.TaskSubmitReq) int {
	if request.Duration > 0 {
		return request.Duration
	}
	seconds, _ := strconv.Atoi(request.Seconds)
	return seconds
}

// novaReelInput builds Nova Reel's input: one 6-second shot, optionally from an image, or on
// v1:1 a multi-shot video of 12 to 120 seconds in steps of 6. Only 1280x720 at 24 fps exists.
func novaReelInput(modelID string, request relaycommon.TaskSubmitReq) (map[string]any, int, error) {
	seconds := taskSeconds(request)
	if seconds == 0 {
		seconds = 6
	}
	multiShot := strings.Contains(modelID, "nova-reel-v1:1")
	if seconds != 6 && (!multiShot || seconds%6 != 0 || seconds < 12 || seconds > 120) {
		return nil, 0, errors.New("seconds must be 6, or on amazon.nova-reel-v1:1 a multiple of 6 from 12 to 120")
	}
	if request.Size != "" && request.Size != "1280x720" {
		return nil, 0, errors.New("size must be 1280x720")
	}
	config := map[string]any{"durationSeconds": seconds, "fps": 24, "dimension": "1280x720"}
	if seed, present := request.Metadata["seed"]; present {
		value, ok := seed.(float64)
		if !ok || value < 0 || value > 2147483646 || value != float64(int64(value)) {
			return nil, 0, errors.New("seed must be an integer from 0 to 2147483646")
		}
		config["seed"] = int64(value)
	}
	if seconds > 6 {
		if request.HasImage() {
			return nil, 0, errors.New("an image can start only a 6-second video")
		}
		return map[string]any{
			"taskType":                 "MULTI_SHOT_AUTOMATED",
			"multiShotAutomatedParams": map[string]any{"text": request.Prompt},
			"videoGenerationConfig":    config,
		}, seconds, nil
	}
	params := map[string]any{"text": request.Prompt}
	if request.HasImage() {
		image, err := novaReelImage(request.Images[0])
		if err != nil {
			return nil, 0, err
		}
		params["images"] = []any{image}
	}
	return map[string]any{"taskType": "TEXT_VIDEO", "textToVideoParams": params, "videoGenerationConfig": config}, seconds, nil
}

// novaReelImage takes the image as a base64 data URL; Bedrock does not fetch URLs.
func novaReelImage(image string) (map[string]any, error) {
	header, data, found := strings.Cut(image, ",")
	mediaType, isBase64 := strings.CutSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if !found || !strings.HasPrefix(header, "data:") || !isBase64 {
		return nil, errors.New("the image must be a base64 data URL")
	}
	format, ok := map[string]string{"image/png": "png", "image/jpeg": "jpeg"}[mediaType]
	if !ok {
		return nil, errors.New("the image must be PNG or JPEG")
	}
	return map[string]any{"format": format, "source": map[string]any{"bytes": data}}, nil
}

var (
	lumaAspectRatios = []string{"1:1", "16:9", "9:16", "4:3", "3:4", "21:9", "9:21"}
	lumaSizes        = map[string][2]string{
		"1280x720": {"16:9", "720p"}, "720x1280": {"9:16", "720p"}, "720x720": {"1:1", "720p"},
		"960x540": {"16:9", "540p"}, "540x960": {"9:16", "540p"}, "540x540": {"1:1", "540p"},
	}
)

// lumaRayInput builds Luma Ray's input: a 5- or 9-second text-to-video at 540p or 720p.
func lumaRayInput(request relaycommon.TaskSubmitReq) (map[string]any, int, error) {
	if request.HasImage() {
		return nil, 0, errors.New("luma.ray-v2:0 generates video from text only")
	}
	seconds := taskSeconds(request)
	if seconds == 0 {
		seconds = 5
	}
	if seconds != 5 && seconds != 9 {
		return nil, 0, errors.New("seconds must be 5 or 9")
	}
	aspectRatio, resolution := "16:9", "720p"
	if request.Size != "" {
		size, ok := lumaSizes[request.Size]
		if !ok {
			return nil, 0, errors.New("size must be one of 1280x720, 720x1280, 720x720, 960x540, 540x960 or 540x540")
		}
		aspectRatio, resolution = size[0], size[1]
	}
	if value, present := request.Metadata["aspect_ratio"]; present {
		ratio, _ := value.(string)
		if !slices.Contains(lumaAspectRatios, ratio) {
			return nil, 0, fmt.Errorf("aspect_ratio must be one of %s", strings.Join(lumaAspectRatios, ", "))
		}
		aspectRatio = ratio
	}
	if value, present := request.Metadata["resolution"]; present {
		chosen, _ := value.(string)
		if chosen != "540p" && chosen != "720p" {
			return nil, 0, errors.New("resolution must be 540p or 720p")
		}
		resolution = chosen
	}
	input := map[string]any{"prompt": request.Prompt, "aspect_ratio": aspectRatio, "duration": fmt.Sprintf("%ds", seconds), "resolution": resolution}
	if value, present := request.Metadata["loop"]; present {
		loop, ok := value.(bool)
		if !ok {
			return nil, 0, errors.New("loop must be true or false")
		}
		input["loop"] = loop
	}
	return input, seconds, nil
}

// EstimateBilling bills the video's length, already bounded to what the model can generate.
func (a *AsyncVideoAdaptor) EstimateBilling(*gin.Context, *relaycommon.RelayInfo) map[string]float64 {
	return map[string]float64{"seconds": float64(a.seconds)}
}

func (a *AsyncVideoAdaptor) BuildRequestURL(*relaycommon.RelayInfo) (string, error) {
	return "", nil
}

func (a *AsyncVideoAdaptor) BuildRequestHeader(*gin.Context, *http.Request, *relaycommon.RelayInfo) error {
	return nil
}

func (a *AsyncVideoAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	bucket := strings.TrimSpace(common.GetEnvOrDefaultString(AsyncOutputBucketEnv, ""))
	if bucket == "" {
		return nil, fmt.Errorf("%s is not set", AsyncOutputBucketEnv)
	}
	request := asyncStartRequest{
		ClientRequestToken: strings.TrimPrefix(info.PublicTaskID, "task_"),
		ModelID:            info.UpstreamModelName,
		ModelInput:         a.modelInput,
	}
	request.OutputDataConfig.S3OutputDataConfig.S3URI = "s3://" + bucket + "/" + info.PublicTaskID
	body, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

// DoRequest starts the invocation. Its answer is Bedrock's, as an HTTP response: the
// invocation ARN, or the status and message of a refusal.
func (a *AsyncVideoAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	var request asyncStartRequest
	if err := common.DecodeJson(requestBody, &request); err != nil {
		return nil, err
	}
	httpClient, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), asyncCallTimeout)
	defer cancel()
	client, _, err := newAsyncClient(ctx, info.ApiKey, httpClient)
	if err != nil {
		return nil, err
	}
	out, err := client.StartAsyncInvoke(ctx, &bedrockruntime.StartAsyncInvokeInput{
		ClientRequestToken: aws.String(request.ClientRequestToken),
		ModelId:            aws.String(request.ModelID),
		ModelInput:         document.NewLazyDocument(request.ModelInput),
		OutputDataConfig: &bedrockruntimeTypes.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig{
			Value: bedrockruntimeTypes.AsyncInvokeS3OutputDataConfig{S3Uri: aws.String(request.OutputDataConfig.S3OutputDataConfig.S3URI)},
		},
	})
	if err != nil {
		return asyncErrorResponse(err)
	}
	body, err := common.Marshal(asyncInvocation{InvocationArn: aws.ToString(out.InvocationArn), Status: string(bedrockruntimeTypes.AsyncInvokeStatusInProgress)})
	if err != nil {
		return nil, err
	}
	return asyncResponse(http.StatusOK, body), nil
}

func (a *AsyncVideoAdaptor) ParseResponse(_ *gin.Context, resp *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *taskdto.TaskError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	var invocation asyncInvocation
	if err := common.Unmarshal(body, &invocation); err != nil || invocation.InvocationArn == "" {
		return nil, service.TaskErrorWrapperLocal(errors.New("Bedrock returned no invocation ARN"), "invalid_response", http.StatusBadGateway)
	}
	return &channel.TaskSubmitResponse{UpstreamTaskID: invocation.InvocationArn, TaskData: body}, nil
}

// FetchTask reads the invocation's state. The key is the channel's, possibly still a reference
// to a secret; its last part is the region.
func (a *AsyncVideoAdaptor) FetchTask(_, key string, task *model.Task, proxy string) (*http.Response, error) {
	httpClient, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), asyncCallTimeout)
	defer cancel()
	client, region, err := newAsyncClient(ctx, key, httpClient)
	if err != nil {
		return nil, err
	}
	out, err := client.GetAsyncInvoke(ctx, &bedrockruntime.GetAsyncInvokeInput{InvocationArn: aws.String(task.GetUpstreamTaskID())})
	if err != nil {
		return asyncErrorResponse(err)
	}
	invocation := asyncInvocation{
		InvocationArn:  aws.ToString(out.InvocationArn),
		ModelArn:       aws.ToString(out.ModelArn),
		Status:         string(out.Status),
		FailureMessage: aws.ToString(out.FailureMessage),
		SubmitTime:     out.SubmitTime,
		EndTime:        out.EndTime,
		Region:         region,
	}
	if s3Output, ok := out.OutputDataConfig.(*bedrockruntimeTypes.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig); ok {
		invocation.OutputS3URI = aws.ToString(s3Output.Value.S3Uri)
	}
	body, err := common.Marshal(invocation)
	if err != nil {
		return nil, err
	}
	return asyncResponse(http.StatusOK, body), nil
}

func (a *AsyncVideoAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, body []byte) (*relaycommon.TaskInfo, error) {
	var invocation asyncInvocation
	if err := common.Unmarshal(body, &invocation); err != nil {
		return nil, err
	}
	switch bedrockruntimeTypes.AsyncInvokeStatus(invocation.Status) {
	case bedrockruntimeTypes.AsyncInvokeStatusInProgress:
		return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}, nil
	case bedrockruntimeTypes.AsyncInvokeStatusCompleted:
		return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: taskcommon.ProgressComplete}, nil
	case bedrockruntimeTypes.AsyncInvokeStatusFailed:
		reason := invocation.FailureMessage
		if reason == "" {
			reason = "Bedrock could not generate the video"
		}
		return relaycommon.FailTaskInfo(reason), nil
	}
	return &relaycommon.TaskInfo{Status: model.TaskStatusUnknown, Reason: "unknown Bedrock invocation status " + invocation.Status}, nil
}

// ConvertToOpenAIVideo answers GET /v1/videos/:id; the URL is the one the fetch presigned.
func (a *AsyncVideoAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := task.ToOpenAIVideo()
	if task.PrivateData.BillingContext != nil {
		if seconds := task.PrivateData.BillingContext.OtherRatios["seconds"]; seconds > 0 {
			video.Seconds = strconv.Itoa(int(seconds))
		}
	}
	switch task.Status {
	case model.TaskStatusSuccess:
		video.SetMetadata("url", task.GetResultURL())
	case model.TaskStatusFailure:
		video.Error = &dto.OpenAIVideoError{Message: task.FailReason, Code: "video_generation_failed"}
	}
	return common.Marshal(video)
}

// PresignResult signs a short-lived GET URL for a finished task's video. The URL carries the
// signature and the task role's session token, never the provider's key.
func (a *AsyncVideoAdaptor) PresignResult(ctx context.Context, task *model.Task) (string, error) {
	var invocation asyncInvocation
	if err := common.Unmarshal(task.Data, &invocation); err != nil {
		return "", err
	}
	location, found := strings.CutPrefix(invocation.OutputS3URI, "s3://")
	bucket, prefix, _ := strings.Cut(location, "/")
	if !found || bucket == "" || invocation.Region == "" {
		return "", errors.New("the task has no output location")
	}
	taskRole, err := asyncTaskRoleCredentials(ctx)
	if err != nil {
		return "", err
	}
	presigner := s3.NewPresignClient(s3.New(s3.Options{Region: invocation.Region, Credentials: taskRole}))
	key := strings.TrimSuffix(prefix, "/") + "/" + asyncVideoObject
	signed, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}, s3.WithPresignExpires(asyncResultURLTTL))
	if err != nil {
		return "", err
	}
	return signed.URL, nil
}

func (a *AsyncVideoAdaptor) GetModelList() []string {
	return []string{"amazon.nova-reel-v1:0", "amazon.nova-reel-v1:1", "luma.ray-v2:0"}
}

func (a *AsyncVideoAdaptor) GetChannelName() string {
	return ChannelName
}

// newAsyncClient builds a Bedrock runtime client for the channel's key: "<ak>|<sk>|<region>"
// signs with that key; an API key or a secret reference ("…|<region>") signs with the task role.
func newAsyncClient(ctx context.Context, key string, httpClient *http.Client) (*bedrockruntime.Client, string, error) {
	if model.ResolveChannelKey != nil {
		if resolved, handled, err := model.ResolveChannelKey(key); handled {
			if err != nil {
				return nil, "", err
			}
			key = resolved
		}
	}
	parts := strings.Split(key, "|")
	region := parts[len(parts)-1]
	if len(parts) < 2 || region == "" || strings.Trim(region, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return nil, "", errors.New("the AWS key names no region")
	}
	var signer aws.CredentialsProvider
	if len(parts) == 3 {
		signer = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(parts[0], parts[1], ""))
	} else {
		taskRole, err := asyncTaskRoleCredentials(ctx)
		if err != nil {
			return nil, "", err
		}
		signer = taskRole
	}
	options := bedrockruntime.Options{Region: region, Credentials: signer}
	if httpClient != nil {
		options.HTTPClient = httpClient
	}
	if asyncBaseEndpoint != "" {
		options.BaseEndpoint = aws.String(asyncBaseEndpoint)
	}
	return bedrockruntime.New(options), region, nil
}

func asyncResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

// asyncErrorResponse turns Bedrock's refusal into its HTTP status and message, so the task
// framework retries, fails or reports it as it does any provider's answer.
func asyncErrorResponse(err error) (*http.Response, error) {
	var apiErr smithy.APIError
	var httpErr interface{ HTTPStatusCode() int }
	if !errors.As(err, &apiErr) || !errors.As(err, &httpErr) {
		return nil, err
	}
	status := httpErr.HTTPStatusCode()
	body, marshalErr := common.Marshal(map[string]string{"code": apiErr.ErrorCode(), "message": apiErr.ErrorMessage()})
	if marshalErr != nil {
		return nil, err
	}
	return asyncResponse(status, body), nil
}
