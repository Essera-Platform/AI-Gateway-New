// Modified by Essera, 2026-11: every Bedrock model through Converse and ConverseStream,
// /v1/responses, embeddings, Bedrock error mapping and the inference profile retry (see essera_*.go).
package aws

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/pkg/errors"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	AwsClient  *bedrockruntime.Client
	AwsModelId string
	AwsReq     any

	// Modified by Essera, 2026-11: state of the Bedrock Runtime REST routes.
	route          bedrockRoute
	modelID        string
	replies        []bedrockReply
	stream         *http.Response
	embeddingPlan  *embeddingPlan
	encodingFormat string
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	// Modified by Essera, 2026-11: a model other than Claude answers Anthropic
	// Messages through Converse.
	if selectBedrockRoute(info) == routeConverse {
		result, err := service.ConvertRequestByID(c, info, relayconvert.ConverterClaudeMessagesToOpenAIChat, request)
		if err != nil {
			return nil, err
		}
		chatRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
		if !ok {
			return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
		}
		return convertOpenAIToConverse(c, getAwsModelID(info.UpstreamModelName), chatRequest)
	}
	claudeAdaptor := claude.Adaptor{}
	if _, err := claudeAdaptor.ConvertClaudeRequest(c, info, request); err != nil {
		return nil, err
	}
	for i, message := range request.Messages {
		updated := false
		if !message.IsStringContent() {
			content, err := message.ParseContent()
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse message content")
			}
			for i2, mediaMessage := range content {
				if mediaMessage.Source != nil {
					if mediaMessage.Source.Type == "url" {
						// 使用统一的文件服务获取图片数据
						source := types.NewURLFileSource(mediaMessage.Source.Url)
						base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Claude")
						if err != nil {
							return nil, fmt.Errorf("get file base64 from url failed: %s", err.Error())
						}
						mediaMessage.Source.MediaType = mimeType
						mediaMessage.Source.Data = base64Data
						mediaMessage.Source.Url = ""
						mediaMessage.Source.Type = "base64"
						content[i2] = mediaMessage
						updated = true
					}
				}
			}
			if updated {
				message.SetContent(content)
			}
		}
		if updated {
			request.Messages[i] = message
		}
	}
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.ChannelOtherSettings.AwsApiType == dto.AwsApiTypeMantle {
		return mantleRequestURL(info)
	}
	// InvokeModel goes through the AWS SDK, which builds the URL from the region
	// in the key. Both key types work there: "<api-key>|<region>" and
	// "<ak>|<sk>|<region>".
	return "", nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	claude.CommonClaudeHeadersOperation(c, req, info)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	// Modified by Essera, 2026-11: every model but Claude, Amazon Nova included,
	// goes through Converse.
	if selectBedrockRoute(info) == routeConverse {
		return convertOpenAIToConverse(c, getAwsModelID(info.UpstreamModelName), request)
	}

	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, request)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert openai request to claude request")
	}
	claudeReq, ok := result.Value.(*dto.ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("expected Anthropic Messages request, got %T", result.Value)
	}
	info.UpstreamModelName = claudeReq.Model
	return claudeReq, err
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	// Modified by Essera, 2026-11: Amazon Rerank and Cohere Rerank are not
	// offered in eu-west-1, the region Essera serves, so rerank is refused.
	return nil, types.NewErrorWithStatusCode(errors.New("rerank is not supported on AWS Bedrock channels"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	// Modified by Essera, 2026-11: Titan Text v2, Titan Multimodal and Cohere Embed v3/v4.
	a.encodingFormat = request.EncodingFormat
	return convertEmbeddingRequest(getAwsModelID(info.UpstreamModelName), request)
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	// Modified by Essera, 2026-11: Responses go through Converse for every model.
	result, err := service.ConvertRequestByID(c, info, relayconvert.ConverterOpenAIResponsesToOpenAIChat, &request)
	if err != nil {
		return nil, err
	}
	chatRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	return convertOpenAIToConverse(c, getAwsModelID(info.UpstreamModelName), chatRequest)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	switch info.ChannelOtherSettings.AwsApiType {
	case "", dto.AwsApiTypeInvokeModel, dto.AwsApiTypeMantle:
	default:
		return nil, errors.New("invalid AWS API type")
	}
	// Modified by Essera, 2026-11: Converse and embeddings use the Bedrock
	// Runtime REST API; Claude keeps InvokeModel through the SDK, or Mantle.
	a.route = selectBedrockRoute(info)
	switch a.route {
	case routeMantle:
		req, err := a.newMantleRequest(c, info, requestBody)
		if err != nil {
			return nil, err
		}
		resp, err := channel.DoRequest(c, req, info)
		if err != nil {
			return nil, newAwsInvokeError(c.Request.Context(), err, "Mantle Messages")
		}
		return resp, nil
	case routeClaudeInvoke:
		return doAwsClientRequest(c, info, a, requestBody)
	}
	return nil, a.doRuntimeRequest(c, info, requestBody)
}

// doRuntimeRequest sends the native body and keeps Bedrock's answers for
// DoResponse, which maps an error status. Modified by Essera, 2026-11.
func (a *Adaptor) doRuntimeRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) error {
	creds, err := parseBedrockCredentials(info.ApiKey)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	a.modelID = resolveBedrockModelID(info.ChannelId, info.UpstreamModelName, creds.region)
	body, err := nativeRequestBody(info, requestBody)
	if err != nil {
		return err
	}
	send := func(action string, payload []byte, stream bool) error {
		// At most two calls: a profile id never gets another retry.
		for {
			resp, err := sendBedrockRuntime(c, info, a.modelID, action, payload, stream)
			if err != nil {
				return err
			}
			if stream && resp.StatusCode == http.StatusOK {
				a.stream = resp
				return nil
			}
			reply, err := readBedrockReply(resp)
			if err != nil {
				return err
			}
			if reply.status != http.StatusOK {
				if profileID := inferenceProfileRetry(c, info.ChannelId, creds.region, a.modelID, bedrockReplyError(reply)); profileID != "" {
					a.modelID = profileID
					continue
				}
			}
			a.replies = append(a.replies, reply)
			return nil
		}
	}
	if a.route == routeConverse {
		if info.IsStream {
			return send("converse-stream", body, true)
		}
		return send("converse", body, false)
	}
	var plan embeddingPlan
	if err := common.Unmarshal(body, &plan); err != nil || len(plan.Calls) == 0 {
		return types.NewErrorWithStatusCode(errors.New("invalid AWS Bedrock embedding request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	a.embeddingPlan = &plan
	for _, call := range plan.Calls {
		if err := send("invoke", call.Body, false); err != nil {
			return err
		}
		if a.replies[len(a.replies)-1].status != http.StatusOK {
			return nil
		}
	}
	return nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	// Modified by Essera, 2026-11: the Bedrock Runtime REST routes.
	route := selectBedrockRoute(info)
	switch route {
	case routeMantle:
		claudeAdaptor := claude.Adaptor{}
		return claudeAdaptor.DoResponse(c, resp, info)
	case routeClaudeInvoke:
		if info.IsStream {
			err, usage = awsStreamHandler(c, info, a)
		} else {
			err, usage = awsHandler(c, info, a)
		}
		return
	}
	for _, reply := range a.replies {
		if reply.status != http.StatusOK {
			return nil, bedrockReplyError(reply)
		}
	}
	if route == routeConverse {
		var chat *http.Response
		if a.stream != nil {
			chat, err = converseStreamAsOpenAI(c, info, a.stream)
		} else {
			chat, err = converseResponseAsOpenAI(c, info, a.replies[0])
		}
		if err != nil {
			return nil, err
		}
		return serveOpenAIChat(c, info, chat)
	}
	response, apiErr := embeddingResponseFromReplies(info.UpstreamModelName, embeddingFamilyOf(a.modelID), a.embeddingPlan, a.replies, a.encodingFormat)
	if apiErr != nil {
		return nil, apiErr
	}
	if response.Usage.PromptTokens == 0 {
		estimate := info.GetEstimatePromptTokens()
		response.Usage = dto.Usage{PromptTokens: estimate, TotalTokens: estimate}
	}
	c.JSON(http.StatusOK, response)
	return &response.Usage, nil
}

func (a *Adaptor) GetModelList() (models []string) {
	for n := range awsModelIDMap {
		models = append(models, n)
	}

	return
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
