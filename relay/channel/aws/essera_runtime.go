// Essera fork: calls the Bedrock Runtime REST API (Converse, ConverseStream and
// InvokeModel with each embedding model's native body) over the channel's HTTP
// client, and maps Bedrock's errors to OpenAI-style statuses.
package aws

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/MoeForge/moego/ojson"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/encoding/httpbinding"
	"github.com/gin-gonic/gin"
)

// bedrockRoute is how a request reaches Bedrock.
type bedrockRoute int

const (
	routeClaudeInvoke bedrockRoute = iota // Anthropic Messages body through the SDK's InvokeModel
	routeMantle
	routeConverse
	routeEmbedding
)

const bedrockInputTokenHeader = "X-Amzn-Bedrock-Input-Token-Count"

// bedrockReply is a Bedrock Runtime answer read in full.
type bedrockReply struct {
	status int
	header http.Header
	body   []byte
}

func selectBedrockRoute(info *relaycommon.RelayInfo) bedrockRoute {
	switch info.RelayMode {
	case relayconstant.RelayModeEmbeddings:
		return routeEmbedding
	case relayconstant.RelayModeResponses:
		return routeConverse
	}
	if info.ChannelOtherSettings.AwsApiType == dto.AwsApiTypeMantle {
		return routeMantle
	}
	if isClaudeModel(getAwsModelID(info.UpstreamModelName)) {
		return routeClaudeInvoke
	}
	return routeConverse
}

// isClaudeModel reports whether a Bedrock model id names an Anthropic model,
// which keeps the native Anthropic Messages body on InvokeModel.
func isClaudeModel(modelID string) bool {
	return strings.Contains(modelID, "anthropic.") || strings.HasPrefix(modelID, "claude-")
}

type bedrockCredentials struct {
	apiKey    string
	accessKey string
	secretKey string
	region    string
}

func parseBedrockCredentials(key string) (bedrockCredentials, error) {
	parts := strings.Split(key, "|")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return bedrockCredentials{}, errors.New("invalid AWS credentials: empty credential or region")
		}
	}
	var creds bedrockCredentials
	switch len(parts) {
	case 2:
		creds = bedrockCredentials{apiKey: parts[0], region: parts[1]}
	case 3:
		creds = bedrockCredentials{accessKey: parts[0], secretKey: parts[1], region: parts[2]}
	default:
		return bedrockCredentials{}, errors.New("invalid AWS credentials: use APIKey|Region or AccessKey|SecretAccessKey|Region")
	}
	for _, ch := range creds.region {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			return bedrockCredentials{}, errors.New("invalid AWS region")
		}
	}
	return creds, nil
}

// resolveBedrockModelID applies the short-name map and the region's
// geographic inference profile to a model that is known to need it: listed in
// the cross-region table, or refused on demand before on this channel. It
// never picks global., which may run outside the region's geography.
func resolveBedrockModelID(channelID int, upstreamModel, region string) string {
	modelID := getAwsModelID(upstreamModel)
	if awsModelCanCrossRegion(modelID, getAwsRegionPrefix(region)) {
		return awsModelCrossRegion(modelID, getAwsRegionPrefix(region))
	}
	if _, known := profileOnlyModels.Load(profileOnlyModelKey(channelID, region, modelID)); known {
		return regionalProfileID(modelID, region)
	}
	return modelID
}

// profileOnlyModels holds the models Bedrock has refused on demand per channel
// and region, so later calls go straight to the region's inference profile.
var profileOnlyModels sync.Map

func profileOnlyModelKey(channelID int, region, modelID string) string {
	return strconv.Itoa(channelID) + "|" + region + "|" + modelID
}

// inferenceProfilePrefixes start a model id that already names an inference
// profile; Bedrock's own profile ids and ARNs are never prefixed again.
var inferenceProfilePrefixes = []string{"arn:", "us.", "us-gov.", "eu.", "apac.", "jp.", "au.", "ca.", "global."}

// regionalProfileID is modelID through the geographic inference profile of
// region (us. for us-east-1), or "" when the id already names a profile or the
// region has none. It is never global., which may run outside the geography.
func regionalProfileID(modelID, region string) string {
	for _, prefix := range inferenceProfilePrefixes {
		if strings.HasPrefix(modelID, prefix) {
			return ""
		}
	}
	geo, ok := awsRegionCrossModelPrefixMap[getAwsRegionPrefix(region)]
	if !ok {
		return ""
	}
	return geo + "." + modelID
}

// inferenceProfileRetry is the model id to call again when Bedrock refused
// modelID because the model is served in region only through an inference
// profile, and remembers that for the channel. It is "" for any other error,
// for an id that already names a profile, and once the client got any bytes.
func inferenceProfileRetry(c *gin.Context, channelID int, region, modelID string, apiErr *types.NewAPIError) string {
	if apiErr == nil || apiErr.GetErrorCode() != "invalid_request" || c.Writer.Written() {
		return ""
	}
	message := strings.ToLower(apiErr.Error())
	if !strings.Contains(message, "on-demand throughput") || !strings.Contains(message, "inference profile") {
		return ""
	}
	profileID := regionalProfileID(modelID, region)
	if profileID != "" {
		profileOnlyModels.Store(profileOnlyModelKey(channelID, region, modelID), struct{}{})
	}
	return profileID
}

func bedrockRuntimeURL(info *relaycommon.RelayInfo, region, modelID, action string) (*url.URL, error) {
	base := "https://bedrock-runtime." + region + ".amazonaws.com"
	// A Mantle channel's base URL names the Mantle endpoint, never the runtime.
	if info.ChannelBaseUrl != "" && info.ChannelOtherSettings.AwsApiType != dto.AwsApiTypeMantle {
		base = strings.TrimRight(info.ChannelBaseUrl, "/")
	}
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("AWS Bedrock base URL must be an HTTPS URL without credentials, query, or fragment")
	}
	// Model ids carry ":" and inference profile ARNs carry "/"; escape them as
	// the SDK does so that SigV4 signs the path Bedrock sees.
	escapedModel := httpbinding.EscapePath(modelID, true)
	endpoint.RawPath = strings.TrimRight(endpoint.EscapedPath(), "/") + "/model/" + escapedModel + "/" + action
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/model/" + modelID + "/" + action
	return endpoint, nil
}

// newBedrockRuntimeRequest builds a signed Bedrock Runtime request: a Bearer
// API key, or SigV4 over the exact payload for an access key pair.
func newBedrockRuntimeRequest(c *gin.Context, info *relaycommon.RelayInfo, modelID, action string, body []byte, stream bool) (*http.Request, error) {
	creds, err := parseBedrockCredentials(info.ApiKey)
	if err != nil {
		return nil, err
	}
	endpoint, err := bedrockRuntimeURL(info, creds.region, modelID, action)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create AWS Bedrock request: %w", err)
	}
	req.URL = endpoint
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if creds.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+creds.apiKey)
		return req, nil
	}
	hash := sha256.Sum256(body)
	signer := v4.NewSigner()
	credentials := aws.Credentials{AccessKeyID: creds.accessKey, SecretAccessKey: creds.secretKey}
	if err := signer.SignHTTP(req.Context(), credentials, req, hex.EncodeToString(hash[:]), "bedrock", creds.region, time.Now()); err != nil {
		return nil, fmt.Errorf("sign AWS Bedrock request: %w", err)
	}
	return req, nil
}

// sendBedrockRuntime posts body to the model's action. A transport failure is
// an error; any HTTP answer, including an error status, is returned.
func sendBedrockRuntime(c *gin.Context, info *relaycommon.RelayInfo, modelID, action string, body []byte, stream bool) (*http.Response, error) {
	req, err := newBedrockRuntimeRequest(c, info, modelID, action, body, stream)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelAwsClientError, http.StatusInternalServerError)
	}
	resp, err := channel.DoRequest(c, req, info)
	if err != nil {
		options := []types.NewAPIErrorOptions{}
		if c.Request.Context().Err() != nil {
			options = append(options, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError, options...)
	}
	if requestID := resp.Header.Get("X-Amzn-Requestid"); requestID != "" {
		c.Set(common.UpstreamRequestIdKey, requestID)
	}
	return resp, nil
}

func readBedrockReply(resp *http.Response) (bedrockReply, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return bedrockReply{}, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	return bedrockReply{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// invokeInputTokens is the input token count Bedrock reports in the
// InvokeModel response headers; 0 when the header is absent or malformed.
func invokeInputTokens(header http.Header) int {
	count, err := strconv.Atoi(strings.TrimSpace(header.Get(bedrockInputTokenHeader)))
	if err != nil || count < 0 || count > maxBedrockTokenCount {
		return 0
	}
	return count
}

// maxBedrockTokenCount bounds a token count read from a Bedrock answer before
// it reaches usage, so an absurd value cannot inflate a charge.
const maxBedrockTokenCount = 1 << 30

// nativeRequestBody is the body for a native Bedrock API. A pass-through
// request carries the client's native body, without model and stream.
func nativeRequestBody(info *relaycommon.RelayInfo, requestBody io.Reader) ([]byte, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("read request body: %w", err), types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if !model_setting.GetGlobalSettings().PassThroughRequestEnabled && !info.ChannelSetting.PassThroughBodyEnabled {
		return body, nil
	}
	if !ojson.ValidBytes(body) || !ojson.GetBytesNoCopy(body, "@this").IsObject() {
		return nil, types.NewErrorWithStatusCode(errors.New("pass-through request body must be a JSON object"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	doc := ojson.NewDoc(body)
	for _, key := range []string{"model", "stream"} {
		if err := relaycommon.DeleteJSONMember(doc, key); err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	return doc.Bytes(), nil
}

// bedrockFault is how a Bedrock exception reaches the client: the status that
// drives retries and channel health, and the OpenAI error type and code.
type bedrockFault struct {
	status    int
	errorType string
	code      string
	skipRetry bool
}

var bedrockFaults = map[string]bedrockFault{
	"throttlingexception":           {status: http.StatusTooManyRequests, errorType: "rate_limit_error", code: "rate_limit_exceeded"},
	"servicequotaexceededexception": {status: http.StatusTooManyRequests, errorType: "rate_limit_error", code: "quota_exceeded"},
	"modelnotreadyexception":        {status: http.StatusServiceUnavailable, errorType: "server_error", code: "model_not_ready"},
	"serviceunavailableexception":   {status: http.StatusServiceUnavailable, errorType: "server_error", code: "service_unavailable"},
	"internalserverexception":       {status: http.StatusInternalServerError, errorType: "server_error", code: "internal_server_error"},
	"modeltimeoutexception":         {status: http.StatusGatewayTimeout, errorType: "server_error", code: "model_timeout"},
	"modelerrorexception":           {status: http.StatusBadGateway, errorType: "server_error", code: "model_error"},
	"modelstreamerrorexception":     {status: http.StatusBadGateway, errorType: "server_error", code: "model_stream_error"},
	"validationexception":           {status: http.StatusBadRequest, errorType: "invalid_request_error", code: "invalid_request", skipRetry: true},
	"accessdeniedexception":         {status: http.StatusForbidden, errorType: "permission_error", code: "access_denied"},
	"resourcenotfoundexception":     {status: http.StatusNotFound, errorType: "invalid_request_error", code: "model_not_found"},
	"unrecognizedclientexception":   {status: http.StatusUnauthorized, errorType: "authentication_error", code: "invalid_api_key"},
	"invalidsignatureexception":     {status: http.StatusUnauthorized, errorType: "authentication_error", code: "invalid_api_key"},
	"incompletesignatureexception":  {status: http.StatusUnauthorized, errorType: "authentication_error", code: "invalid_api_key"},
	"expiredtokenexception":         {status: http.StatusUnauthorized, errorType: "authentication_error", code: "invalid_api_key"},
}

// newBedrockError maps a Bedrock exception name (as in x-amzn-ErrorType, the
// event stream's :exception-type, or the SDK's ErrorCode) to an OpenAI-style
// error. An unknown name keeps the upstream status.
func newBedrockError(name, message string, status int) *types.NewAPIError {
	name, _, _ = strings.Cut(name, ":")
	if _, after, found := strings.Cut(name, "#"); found {
		name = after
	}
	fault, known := bedrockFaults[strings.ToLower(strings.TrimSpace(name))]
	if !known {
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		fault = bedrockFault{status: status, errorType: "upstream_error", code: "bedrock_error"}
	}
	// Bedrock answers an unknown or unavailable model id with a validation
	// error; another channel may serve the model, so it is a 404.
	lowered := strings.ToLower(message)
	if fault.code == "invalid_request" && (strings.Contains(lowered, "model identifier is invalid") || strings.Contains(lowered, "model is not supported")) {
		fault = bedrockFaults["resourcenotfoundexception"]
	}
	if message == "" {
		message = name
	}
	options := []types.NewAPIErrorOptions{}
	if fault.skipRetry {
		options = append(options, types.ErrOptionWithSkipRetry())
	}
	return types.WithOpenAIError(types.OpenAIError{
		Message: "AWS Bedrock: " + message,
		Type:    fault.errorType,
		Code:    fault.code,
	}, fault.status, options...)
}

// bedrockReplyError maps a non-200 Bedrock Runtime answer.
func bedrockReplyError(reply bedrockReply) *types.NewAPIError {
	var payload struct {
		Message      string `json:"message"`
		MessageUpper string `json:"Message"`
		Type         string `json:"__type"`
	}
	_ = common.Unmarshal(reply.body, &payload)
	name := reply.header.Get("X-Amzn-Errortype")
	if name == "" {
		name = payload.Type
	}
	message := payload.Message
	if message == "" {
		message = payload.MessageUpper
	}
	if message == "" {
		message = strings.TrimSpace(string(reply.body))
	}
	return newBedrockError(name, message, reply.status)
}

// awsSDKError maps an AWS SDK error from the InvokeModel path.
func awsSDKError(err error) *types.NewAPIError {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return nil
	}
	return newBedrockError(apiErr.ErrorCode(), apiErr.ErrorMessage(), getAwsErrorStatusCode(err))
}
