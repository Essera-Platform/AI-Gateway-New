// Essera fork: text for every Bedrock model through the Converse and
// ConverseStream APIs. The client's OpenAI chat request becomes a Converse
// body; Bedrock's answer becomes an OpenAI chat completion (or chunk stream),
// which the OpenAI handlers then serve in the client's own protocol.
package aws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/MoeForge/moego/ojson"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/gin-gonic/gin"
)

type converseRequest struct {
	Messages                     []converseMessage        `json:"messages"`
	System                       []converseContentBlock   `json:"system,omitempty"`
	InferenceConfig              *converseInferenceConfig `json:"inferenceConfig,omitempty"`
	ToolConfig                   *converseToolConfig      `json:"toolConfig,omitempty"`
	AdditionalModelRequestFields map[string]any           `json:"additionalModelRequestFields,omitempty"`
}

type converseMessage struct {
	Role    string                 `json:"role"`
	Content []converseContentBlock `json:"content"`
}

type converseContentBlock struct {
	Text             *string             `json:"text,omitempty"`
	Image            *converseMedia      `json:"image,omitempty"`
	Document         *converseMedia      `json:"document,omitempty"`
	Video            *converseMedia      `json:"video,omitempty"`
	Audio            *converseMedia      `json:"audio,omitempty"`
	ToolUse          *converseToolUse    `json:"toolUse,omitempty"`
	ToolResult       *converseToolResult `json:"toolResult,omitempty"`
	ReasoningContent *converseReasoning  `json:"reasoningContent,omitempty"`
	CachePoint       *converseCachePoint `json:"cachePoint,omitempty"`
}

type converseMedia struct {
	Format string         `json:"format"`
	Name   string         `json:"name,omitempty"`
	Source converseSource `json:"source"`
}

type converseSource struct {
	Bytes      string      `json:"bytes,omitempty"`
	S3Location *converseS3 `json:"s3Location,omitempty"`
}

type converseS3 struct {
	URI string `json:"uri"`
}

type converseToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type converseToolResult struct {
	ToolUseID string                 `json:"toolUseId"`
	Content   []converseContentBlock `json:"content"`
}

type converseReasoning struct {
	ReasoningText *struct {
		Text      string `json:"text"`
		Signature string `json:"signature,omitempty"`
	} `json:"reasoningText,omitempty"`
	RedactedContent string `json:"redactedContent,omitempty"`
}

type converseCachePoint struct {
	Type string `json:"type"`
}

type converseInferenceConfig struct {
	MaxTokens     *uint    `json:"maxTokens,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"topP,omitempty"`
	StopSequences []string `json:"stopSequences,omitempty"`
}

type converseToolConfig struct {
	Tools      []converseTool `json:"tools"`
	ToolChoice map[string]any `json:"toolChoice,omitempty"`
}

type converseTool struct {
	ToolSpec converseToolSpec `json:"toolSpec"`
}

type converseToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema struct {
		JSON json.RawMessage `json:"json"`
	} `json:"inputSchema"`
}

type converseUsage struct {
	InputTokens           int                   `json:"inputTokens"`
	OutputTokens          int                   `json:"outputTokens"`
	CacheReadInputTokens  int                   `json:"cacheReadInputTokens"`
	CacheWriteInputTokens int                   `json:"cacheWriteInputTokens"`
	CacheDetails          []converseCacheDetail `json:"cacheDetails"`
}

// converseCacheDetail is the part of the cache writes made with one TTL, "5m" or "1h".
type converseCacheDetail struct {
	InputTokens int    `json:"inputTokens"`
	TTL         string `json:"ttl"`
}

type converseResponse struct {
	Output struct {
		Message converseMessage `json:"message"`
	} `json:"output"`
	StopReason string         `json:"stopReason"`
	Usage      *converseUsage `json:"usage"`
}

// converseStreamEvent holds the payload of every ConverseStream event type.
type converseStreamEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Start             *struct {
		ToolUse *converseToolUse `json:"toolUse"`
	} `json:"start"`
	Delta *struct {
		Text    *string `json:"text"`
		ToolUse *struct {
			Input string `json:"input"`
		} `json:"toolUse"`
		ReasoningContent *struct {
			Text *string `json:"text"`
		} `json:"reasoningContent"`
	} `json:"delta"`
	StopReason string         `json:"stopReason"`
	Usage      *converseUsage `json:"usage"`
}

func newBadRequest(format string, args ...any) error {
	return types.NewErrorWithStatusCode(fmt.Errorf(format, args...), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// newNovaHighEffortMaxTokensError is a Claude-style error so that its type is
// invalid_request_error in every client protocol, Anthropic Messages included.
func newNovaHighEffortMaxTokensError() error {
	return types.WithClaudeError(types.ClaudeError{
		Type:    "invalid_request_error",
		Message: "Nova 2 at high reasoning effort does not accept max_tokens: remove max_tokens or use medium reasoning effort.",
	}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

var converseImageFormats = map[string]string{"image/png": "png", "image/jpeg": "jpeg", "image/jpg": "jpeg", "image/gif": "gif", "image/webp": "webp"}

var converseDocumentFormats = map[string]string{
	"application/pdf": "pdf", "text/csv": "csv", "text/plain": "txt", "text/markdown": "md", "text/html": "html",
	"application/msword": "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx",
}

var converseVideoFormats = map[string]string{
	"video/mp4": "mp4", "video/quicktime": "mov", "video/x-matroska": "mkv", "video/webm": "webm",
	"video/x-flv": "flv", "video/mpeg": "mpeg", "video/x-ms-wmv": "wmv", "video/3gpp": "three_gp",
}

var converseAudioFormats = map[string]bool{
	"mp3": true, "opus": true, "wav": true, "aac": true, "flac": true, "mp4": true, "ogg": true, "mkv": true,
	"mka": true, "x-aac": true, "m4a": true, "mpeg": true, "mpga": true, "pcm": true, "webm": true,
}

var converseDocumentNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9 \-()\[\]]+`)

// Bedrock's limits on the images and documents of one request.
const (
	bedrockMaxImages        = 20
	bedrockMaxImageBytes    = 3_932_160 // 3.75 MB
	bedrockMaxDocuments     = 5
	bedrockMaxDocumentBytes = 4_718_592 // 4.5 MB
)

// checkBedrockMediaLimits refuses a request with more images or documents
// than Bedrock takes, or an inline one larger than Bedrock takes, so that it
// is not sent at all. Linked media are counted; their size is Bedrock's to
// check.
func checkBedrockMediaLimits(messages []dto.Message) error {
	images, documents := 0, 0
	for i := range messages {
		for _, part := range messages[i].ParseContent() {
			switch part.Type {
			case dto.ContentTypeImageURL:
				images++
				if images > bedrockMaxImages {
					return newBadRequest("Bedrock accepts at most %d images in a request", bedrockMaxImages)
				}
				if image := part.GetImageMedia(); image != nil && inlineMediaBytes(image.Url) > bedrockMaxImageBytes {
					return newBadRequest("image %d is %s; Bedrock accepts images of up to 3.75 MB", images, formatMegabytes(inlineMediaBytes(image.Url)))
				}
			case dto.ContentTypeFile:
				documents++
				if documents > bedrockMaxDocuments {
					return newBadRequest("Bedrock accepts at most %d documents in a request", bedrockMaxDocuments)
				}
				if file := part.GetFile(); file != nil && inlineMediaBytes(file.FileData) > bedrockMaxDocumentBytes {
					return newBadRequest("document %d is %s; Bedrock accepts documents of up to 4.5 MB", documents, formatMegabytes(inlineMediaBytes(file.FileData)))
				}
			}
		}
	}
	return nil
}

// inlineMediaBytes is the decoded size of base64 media, bare or in a data
// URL, and 0 for a link.
func inlineMediaBytes(data string) int {
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") || strings.HasPrefix(data, "s3://") {
		return 0
	}
	if _, encoded, found := strings.Cut(data, ";base64,"); found {
		data = encoded
	}
	return len(strings.TrimRight(data, "=")) * 3 / 4
}

func formatMegabytes(size int) string {
	return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
}

// convertOpenAIToConverse maps an OpenAI chat request to a Converse body.
func convertOpenAIToConverse(c *gin.Context, modelID string, request *dto.GeneralOpenAIRequest) (*converseRequest, error) {
	if request.N != nil && *request.N > 1 {
		return nil, newBadRequest("n must be 1: Bedrock Converse returns one choice")
	}
	maxTokens := request.MaxCompletionTokens
	if maxTokens == nil || *maxTokens == 0 {
		maxTokens = request.MaxTokens
	}
	if maxTokens != nil && *maxTokens == 0 {
		maxTokens = nil
	}
	reasoningFields := converseReasoningFields(modelID, reasoningEffort(request))
	reasoningConfig, isNova := reasoningFields["reasoningConfig"].(map[string]any)
	novaHighEffort := isNova && reasoningConfig["maxReasoningEffort"] == "high"
	// Nova 2 at high effort refuses maxTokens. It is the caller's cost cap, so
	// it is neither dropped nor traded for a lower effort: the request is
	// refused here rather than by Bedrock.
	if novaHighEffort && maxTokens != nil {
		return nil, newNovaHighEffortMaxTokensError()
	}
	converse := &converseRequest{Messages: []converseMessage{}}
	documents := 0
	for i := range request.Messages {
		message := &request.Messages[i]
		switch message.Role {
		case "system", "developer":
			blocks, err := converseContentBlocks(c, message, &documents)
			if err != nil {
				return nil, err
			}
			for _, block := range blocks {
				if block.Text == nil && block.CachePoint == nil {
					return nil, newBadRequest("a system message may carry only text")
				}
			}
			converse.System = append(converse.System, blocks...)
		case "user":
			blocks, err := converseContentBlocks(c, message, &documents)
			if err != nil {
				return nil, err
			}
			converse.appendMessage("user", blocks)
		case "assistant":
			blocks, err := converseContentBlocks(c, message, &documents)
			if err != nil {
				return nil, err
			}
			for _, call := range message.ParseToolCalls() {
				input := json.RawMessage(strings.TrimSpace(call.Function.Arguments))
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				if !ojson.ValidBytes(input) {
					return nil, newBadRequest("tool call %s has arguments that are not JSON", call.ID)
				}
				blocks = append(blocks, converseContentBlock{ToolUse: &converseToolUse{ToolUseID: call.ID, Name: call.Function.Name, Input: input}})
			}
			converse.appendMessage("assistant", blocks)
		case "tool":
			result, err := converseContentBlocks(c, message, &documents)
			if err != nil {
				return nil, err
			}
			if len(result) == 0 {
				result = []converseContentBlock{{Text: common.GetPointer("(empty)")}}
			}
			converse.appendMessage("user", []converseContentBlock{{ToolResult: &converseToolResult{ToolUseID: message.ToolCallId, Content: result}}})
		default:
			return nil, newBadRequest("message role %q is not supported by Bedrock Converse", message.Role)
		}
	}

	temperature, topP := request.Temperature, request.TopP
	// Nova 2 at high effort refuses temperature and topP as well.
	if novaHighEffort {
		temperature, topP = nil, nil
	}
	stop := parseStopSequences(request.Stop)
	if maxTokens != nil || temperature != nil || topP != nil || len(stop) > 0 {
		converse.InferenceConfig = &converseInferenceConfig{MaxTokens: maxTokens, Temperature: temperature, TopP: topP, StopSequences: stop}
	}

	if len(request.Tools) > 0 {
		toolConfig := &converseToolConfig{}
		for _, tool := range request.Tools {
			if tool.Type != "" && tool.Type != "function" {
				return nil, newBadRequest("tool type %q is not supported by Bedrock Converse", tool.Type)
			}
			spec := converseToolSpec{Name: tool.Function.Name, Description: tool.Function.Description}
			spec.InputSchema.JSON = tool.Function.Parameters
			if len(spec.InputSchema.JSON) == 0 || string(spec.InputSchema.JSON) == "null" {
				spec.InputSchema.JSON = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			toolConfig.Tools = append(toolConfig.Tools, converseTool{ToolSpec: spec})
		}
		toolChoice, err := converseToolChoice(request.ToolChoice)
		if err != nil {
			return nil, err
		}
		toolConfig.ToolChoice = toolChoice
		converse.ToolConfig = toolConfig
	}

	if reasoningFields != nil {
		converse.AdditionalModelRequestFields = reasoningFields
	}
	return converse, nil
}

// appendMessage adds blocks under role, joining a run of same-role messages
// (for example several tool results) into one, as Converse alternates roles.
func (r *converseRequest) appendMessage(role string, blocks []converseContentBlock) {
	if len(blocks) == 0 {
		return
	}
	if last := len(r.Messages) - 1; last >= 0 && r.Messages[last].Role == role {
		r.Messages[last].Content = append(r.Messages[last].Content, blocks...)
		return
	}
	r.Messages = append(r.Messages, converseMessage{Role: role, Content: blocks})
}

func converseContentBlocks(c *gin.Context, message *dto.Message, documents *int) ([]converseContentBlock, error) {
	var blocks []converseContentBlock
	parts := message.ParseContent()
	// A part's cache_control, as parsed or from the client's own part when
	// each of them was parsed.
	rawParts, _ := message.Content.([]any)
	cached := func(i int) bool {
		if len(parts[i].CacheControl) > 0 {
			return true
		}
		if len(rawParts) != len(parts) {
			return false
		}
		item, ok := rawParts[i].(map[string]any)
		return ok && item["cache_control"] != nil
	}
	for i, part := range parts {
		var block converseContentBlock
		switch part.Type {
		case dto.ContentTypeText:
			if part.Text == "" {
				continue
			}
			block.Text = common.GetPointer(part.Text)
		case dto.ContentTypeImageURL:
			data, mimeType, err := loadMedia(c, part)
			if err != nil {
				return nil, err
			}
			format, ok := converseImageFormats[mimeType]
			if !ok {
				return nil, newBadRequest("image type %q is not supported; use PNG, JPEG, GIF or WebP", mimeType)
			}
			block.Image = &converseMedia{Format: format, Source: converseSource{Bytes: data}}
		case dto.ContentTypeInputAudio:
			audio := part.GetInputAudio()
			if audio == nil || audio.Data == "" {
				return nil, newBadRequest("input_audio needs data")
			}
			format := strings.ToLower(audio.Format)
			if !converseAudioFormats[format] {
				return nil, newBadRequest("audio format %q is not supported by Bedrock Converse", audio.Format)
			}
			data := audio.Data
			if _, encoded, found := strings.Cut(data, ";base64,"); found && strings.HasPrefix(data, "data:") {
				data = encoded
			}
			block.Audio = &converseMedia{Format: format, Source: converseSource{Bytes: data}}
		case dto.ContentTypeFile:
			data, mimeType, err := loadMedia(c, part)
			if err != nil {
				return nil, err
			}
			format, ok := converseDocumentFormats[strings.SplitN(mimeType, ";", 2)[0]]
			if !ok {
				return nil, newBadRequest("file type %q is not supported by Bedrock Converse", mimeType)
			}
			*documents++
			name := ""
			if file := part.GetFile(); file != nil {
				// Bedrock allows letters, digits, single spaces, hyphens, parentheses and brackets.
				name = strings.Join(strings.Fields(converseDocumentNameUnsafe.ReplaceAllString(file.FileName, " ")), " ")
			}
			if name == "" {
				name = fmt.Sprintf("document %d", *documents)
			}
			block.Document = &converseMedia{Format: format, Name: name, Source: converseSource{Bytes: data}}
		case dto.ContentTypeVideoUrl:
			video := part.GetVideoUrl()
			if video == nil || video.Url == "" {
				return nil, newBadRequest("video_url needs a url")
			}
			if strings.HasPrefix(video.Url, "s3://") {
				format := strings.ToLower(video.Url[strings.LastIndex(video.Url, ".")+1:])
				block.Video = &converseMedia{Format: format, Source: converseSource{S3Location: &converseS3{URI: video.Url}}}
				break
			}
			data, mimeType, err := loadMedia(c, part)
			if err != nil {
				return nil, err
			}
			format, ok := converseVideoFormats[mimeType]
			if !ok {
				return nil, newBadRequest("video type %q is not supported by Bedrock Converse", mimeType)
			}
			block.Video = &converseMedia{Format: format, Source: converseSource{Bytes: data}}
		default:
			return nil, newBadRequest("content type %q is not supported by Bedrock Converse", part.Type)
		}
		blocks = append(blocks, block)
		if cached(i) && cachingDecided(c) != policy.CachingOff {
			blocks = append(blocks, converseContentBlock{CachePoint: &converseCachePoint{Type: "default"}})
		}
	}
	return blocks, nil
}

func loadMedia(c *gin.Context, part dto.MediaContent) (string, string, error) {
	source := part.ToFileSource()
	if source == nil {
		return "", "", newBadRequest("%s content needs data", part.Type)
	}
	data, mimeType, err := service.GetBase64Data(c, source, "formatting media for AWS Bedrock")
	if err != nil {
		return "", "", newBadRequest("load %s content: %s", part.Type, err.Error())
	}
	return data, strings.ToLower(mimeType), nil
}

func converseToolChoice(choice any) (map[string]any, error) {
	switch value := choice.(type) {
	case nil:
		return nil, nil
	case string:
		switch value {
		case "auto", "none":
			// Converse has no "none"; tools stay declared so that earlier tool
			// calls in the conversation remain valid.
			return map[string]any{"auto": map[string]any{}}, nil
		case "required":
			return map[string]any{"any": map[string]any{}}, nil
		}
	case map[string]any:
		if function, ok := value["function"].(map[string]any); ok {
			if name, ok := function["name"].(string); ok && name != "" {
				return map[string]any{"tool": map[string]any{"name": name}}, nil
			}
		}
	}
	return nil, newBadRequest("tool_choice %v is not supported by Bedrock Converse", choice)
}

func reasoningEffort(request *dto.GeneralOpenAIRequest) string {
	if request.ReasoningEffort != "" {
		return request.ReasoningEffort
	}
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if len(request.Reasoning) > 0 && common.Unmarshal(request.Reasoning, &reasoning) == nil {
		return reasoning.Effort
	}
	return ""
}

// converseReasoningFields carries the reasoning effort in the model family's
// own request field. Families that always reason, or whose control Bedrock
// does not document, get nothing.
func converseReasoningFields(modelID, effort string) map[string]any {
	switch effort {
	case "", "none":
		return nil
	case "minimal":
		effort = "low"
	case "xhigh":
		effort = "high"
	}
	if effort != "low" && effort != "medium" && effort != "high" {
		return nil
	}
	switch {
	// GPT-5.6 and later refuse reasoning_effort; every GPT, gpt-oss and Kimi K3
	// take the Responses API's reasoning.effort (checked on Bedrock, 2026-11-23).
	case strings.Contains(modelID, "openai."), strings.Contains(modelID, "moonshotai.kimi-k3"):
		return map[string]any{"reasoning": map[string]any{"effort": effort}}
	case strings.Contains(modelID, "amazon.nova-2"):
		return map[string]any{"reasoningConfig": map[string]any{"type": "enabled", "maxReasoningEffort": effort}}
	}
	return nil
}

func converseFinishReason(stopReason string) string {
	switch stopReason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "guardrail_intervened", "content_filtered":
		return "content_filter"
	}
	return "stop"
}

// openAIUsageFromConverse counts the cache reads and writes, which Converse
// reports apart from inputTokens, as prompt tokens, and keeps the writes'
// split by TTL, which Bedrock prices apart.
func openAIUsageFromConverse(usage *converseUsage) dto.Usage {
	if usage == nil {
		return dto.Usage{}
	}
	bounded := func(n int) int { return min(max(n, 0), maxBedrockTokenCount) }
	input, output := bounded(usage.InputTokens), bounded(usage.OutputTokens)
	cacheRead, cacheWrite := bounded(usage.CacheReadInputTokens), bounded(usage.CacheWriteInputTokens)
	var written5m, written1h int
	for _, detail := range usage.CacheDetails {
		switch detail.TTL {
		case "5m":
			written5m = bounded(written5m + bounded(detail.InputTokens))
		case "1h":
			written1h = bounded(written1h + bounded(detail.InputTokens))
		}
	}
	cacheWrite = max(cacheWrite, written5m+written1h)
	prompt := input + cacheRead + cacheWrite
	result := dto.Usage{PromptTokens: prompt, CompletionTokens: output, TotalTokens: prompt + output}
	result.PromptTokensDetails.CachedTokens = cacheRead
	result.PromptTokensDetails.CacheWriteTokens = cacheWrite
	result.ClaudeCacheCreation1hTokens = written1h
	result.ClaudeCacheCreation5mTokens = cacheWrite - written1h
	return result
}

// openAIChatFromConverse rebuilds a Converse answer as an OpenAI completion.
func openAIChatFromConverse(id, model string, response *converseResponse) *dto.OpenAITextResponse {
	var text, reasoning strings.Builder
	var toolCalls []dto.ToolCallResponse
	for _, block := range response.Output.Message.Content {
		switch {
		case block.Text != nil:
			text.WriteString(*block.Text)
		case block.ReasoningContent != nil && block.ReasoningContent.ReasoningText != nil:
			reasoning.WriteString(block.ReasoningContent.ReasoningText.Text)
		case block.ToolUse != nil:
			arguments := string(block.ToolUse.Input)
			if arguments == "" {
				arguments = "{}"
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID: block.ToolUse.ToolUseID, Type: "function",
				Function: dto.FunctionResponse{Name: block.ToolUse.Name, Arguments: arguments},
			})
		}
	}
	message := dto.Message{Role: "assistant", Content: text.String()}
	if reasoning.Len() > 0 {
		message.ReasoningContent = common.GetPointer(reasoning.String())
	}
	if len(toolCalls) > 0 {
		message.SetToolCalls(toolCalls)
	}
	return &dto.OpenAITextResponse{
		Id: id, Object: "chat.completion", Created: common.GetTimestamp(), Model: model,
		Choices: []dto.OpenAITextResponseChoice{{Index: 0, Message: message, FinishReason: converseFinishReason(response.StopReason)}},
		Usage:   openAIUsageFromConverse(response.Usage),
	}
}

// converseStreamState turns ConverseStream events into OpenAI chunks.
type converseStreamState struct {
	id, model   string
	created     int64
	toolIndexes map[int]int
	nextTool    int
}

func (s *converseStreamState) chunk(delta dto.ChatCompletionsStreamResponseChoiceDelta, finishReason *string) dto.ChatCompletionsStreamResponse {
	return dto.ChatCompletionsStreamResponse{
		Id: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: delta, FinishReason: finishReason}},
	}
}

// apply returns the chunks an event produces, none for events with nothing
// to tell an OpenAI client.
func (s *converseStreamState) apply(eventType string, event *converseStreamEvent) []dto.ChatCompletionsStreamResponse {
	switch eventType {
	case "messageStart":
		return []dto.ChatCompletionsStreamResponse{s.chunk(dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant", Content: common.GetPointer("")}, nil)}
	case "contentBlockStart":
		if event.Start == nil || event.Start.ToolUse == nil {
			return nil
		}
		index := s.nextTool
		s.nextTool++
		s.toolIndexes[event.ContentBlockIndex] = index
		return []dto.ChatCompletionsStreamResponse{s.chunk(dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
			Index: common.GetPointer(index), ID: event.Start.ToolUse.ToolUseID, Type: "function",
			Function: dto.FunctionResponse{Name: event.Start.ToolUse.Name},
		}}}, nil)}
	case "contentBlockDelta":
		if event.Delta == nil {
			return nil
		}
		var delta dto.ChatCompletionsStreamResponseChoiceDelta
		switch {
		case event.Delta.Text != nil:
			delta.Content = event.Delta.Text
		case event.Delta.ReasoningContent != nil && event.Delta.ReasoningContent.Text != nil:
			delta.ReasoningContent = event.Delta.ReasoningContent.Text
		case event.Delta.ToolUse != nil:
			index, ok := s.toolIndexes[event.ContentBlockIndex]
			if !ok {
				return nil
			}
			delta.ToolCalls = []dto.ToolCallResponse{{Index: common.GetPointer(index), Function: dto.FunctionResponse{Arguments: event.Delta.ToolUse.Input}}}
		default:
			return nil
		}
		return []dto.ChatCompletionsStreamResponse{s.chunk(delta, nil)}
	case "messageStop":
		return []dto.ChatCompletionsStreamResponse{s.chunk(dto.ChatCompletionsStreamResponseChoiceDelta{}, common.GetPointer(converseFinishReason(event.StopReason)))}
	case "metadata":
		if event.Usage == nil {
			return nil
		}
		usage := openAIUsageFromConverse(event.Usage)
		final := dto.ChatCompletionsStreamResponse{Id: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model, Choices: []dto.ChatCompletionsStreamResponseChoice{}, Usage: &usage}
		return []dto.ChatCompletionsStreamResponse{final}
	}
	return nil
}

// readConverseEvent decodes the next ConverseStream event; an exception in the
// stream becomes the mapped error.
func readConverseEvent(decoder *eventstream.Decoder, reader io.Reader) (string, *converseStreamEvent, error) {
	message, err := decoder.Decode(reader, nil)
	if err != nil {
		return "", nil, err
	}
	header := func(name string) string {
		if value := message.Headers.Get(name); value != nil {
			return value.String()
		}
		return ""
	}
	switch header(":message-type") {
	case "exception":
		var payload struct {
			Message string `json:"message"`
		}
		_ = common.Unmarshal(message.Payload, &payload)
		return "", nil, newBedrockError(header(":exception-type"), payload.Message, http.StatusBadGateway)
	case "error":
		return "", nil, newBedrockError(header(":error-code"), header(":error-message"), http.StatusBadGateway)
	}
	var event converseStreamEvent
	if err := common.Unmarshal(message.Payload, &event); err != nil {
		return "", nil, types.NewOpenAIError(fmt.Errorf("decode ConverseStream event: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	return header(":event-type"), &event, nil
}

// converseStreamAsOpenAI reads the first ConverseStream event, so that a
// failure before any output stays a retryable error, then pipes the rest as
// OpenAI chunk SSE.
func converseStreamAsOpenAI(c *gin.Context, info *relaycommon.RelayInfo, upstream *http.Response) (*http.Response, *types.NewAPIError) {
	decoder := eventstream.NewDecoder()
	eventType, event, err := readConverseEvent(decoder, upstream.Body)
	if err != nil {
		_ = upstream.Body.Close()
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, types.NewOpenAIError(fmt.Errorf("read ConverseStream: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	info.SetFirstResponseTime()
	state := &converseStreamState{id: helper.GetResponseID(c), model: info.UpstreamModelName, created: common.GetTimestamp(), toolIndexes: map[int]int{}}
	reader, writer := io.Pipe()
	go func() {
		defer upstream.Body.Close()
		for {
			for _, chunk := range state.apply(eventType, event) {
				data, err := common.Marshal(chunk)
				if err != nil {
					_ = writer.CloseWithError(err)
					return
				}
				if _, err := fmt.Fprintf(writer, "data: %s\n\n", data); err != nil {
					return
				}
			}
			eventType, event, err = readConverseEvent(decoder, upstream.Body)
			if errors.Is(err, io.EOF) {
				_, _ = io.WriteString(writer, "data: [DONE]\n\n")
				_ = writer.Close()
				return
			}
			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}
		}
	}()
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
}

// converseResponseAsOpenAI rebuilds a Converse answer as an OpenAI JSON body.
func converseResponseAsOpenAI(c *gin.Context, info *relaycommon.RelayInfo, reply bedrockReply) (*http.Response, *types.NewAPIError) {
	var response converseResponse
	if err := common.Unmarshal(reply.body, &response); err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("decode Converse response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	body, err := common.Marshal(openAIChatFromConverse(helper.GetResponseID(c), info.UpstreamModelName, &response))
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

// serveOpenAIChat serves an OpenAI chat answer in the client's protocol:
// chat completions, Anthropic Messages, Gemini or Responses.
func serveOpenAIChat(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if info.RelayMode == relayconstant.RelayModeResponses {
		if info.IsStream {
			return openai.OaiChatToResponsesStreamHandler(c, info, resp)
		}
		return openai.OaiChatToResponsesHandler(c, info, resp)
	}
	if info.IsStream {
		return openai.OaiStreamHandler(c, info, resp)
	}
	return openai.OpenaiHandler(c, info, resp)
}
