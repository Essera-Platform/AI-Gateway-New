// Essera fork: Amazon Nova Sonic, Bedrock's speech-to-speech model, served through the OpenAI
// Realtime API. The client's WebSocket is bridged to Bedrock's bidirectional stream
// (InvokeModelWithBidirectionalStream), and the session is metered in the speech and text
// tokens Bedrock reports it billed.
package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/metering"
	"github.com/QuantumNous/new-api/essera/settings"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	// Realtime's pcm16 is 16-bit mono PCM at 24 kHz, which Nova Sonic takes in and gives out.
	sonicSampleRate   = 24000
	sonicPromptName   = "prompt"
	sonicDefaultVoice = "matthew"
	// Bedrock receives the client's audio in chunks of at most this many bytes of PCM.
	sonicAudioChunk = 32000
	// The largest WebSocket message a client may send, as OpenAI bounds an audio append.
	sonicClientMessageLimit = 16 << 20
	// How long the session's last events and usage may take to arrive once the client left.
	sonicDrainTimeout = 10 * time.Second
	// The most tokens a client may give one answer; Bedrock accepts it.
	sonicMaxTokensLimit   = 10000
	sonicDefaultMaxTokens = 1024
	// How long an answer's response.done waits for Bedrock's usage event, which follows the
	// answer's last content.
	sonicTurnUsageWait = time.Second
	// Bedrock refuses a prompt without audio only after billing it, so a response asked for
	// before any audio is refused here.
	sonicAudioRequired = "Nova Sonic is a speech model: send audio with input_audio_buffer.append; text can only accompany audio."
)

// openAIVoices are the Realtime API's own voices; Nova Sonic has none of them, so a client
// that names one hears the default voice.
var openAIVoices = []string{"alloy", "ash", "ballad", "coral", "echo", "fable", "onyx", "nova", "sage", "shimmer", "verse", "marin", "cedar"}

func isSonicModel(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), ".nova-") && strings.Contains(strings.ToLower(modelID), "sonic")
}

// sonicStream is one Bedrock bidirectional stream: the events sent and the events received,
// each one JSON document.
type sonicStream interface {
	Send(ctx context.Context, event []byte) error
	Receive() ([]byte, bool)
	CloseSend() error
	Close() error
	Err() error
}

type sdkSonicStream struct {
	stream *bedrockruntime.InvokeModelWithBidirectionalStreamEventStream
}

func (s sdkSonicStream) Send(ctx context.Context, event []byte) error {
	return s.stream.Send(ctx, &bedrockruntimeTypes.InvokeModelWithBidirectionalStreamInputMemberChunk{Value: bedrockruntimeTypes.BidirectionalInputPayloadPart{Bytes: event}})
}

func (s sdkSonicStream) Receive() ([]byte, bool) {
	for event := range s.stream.Events() {
		if chunk, ok := event.(*bedrockruntimeTypes.InvokeModelWithBidirectionalStreamOutputMemberChunk); ok {
			return chunk.Value.Bytes, true
		}
	}
	return nil, false
}

func (s sdkSonicStream) CloseSend() error { return s.stream.Writer.Close() }
func (s sdkSonicStream) Close() error     { return s.stream.Close() }
func (s sdkSonicStream) Err() error       { return s.stream.Err() }

type sonicOpener func(ctx context.Context) (sonicStream, error)

// prepareSonic checks that the Realtime session asks for Nova Sonic and readies the call to
// Bedrock, which the session makes once the client sends its first audio or item.
func (a *Adaptor) prepareSonic(c *gin.Context, info *relaycommon.RelayInfo) error {
	creds, err := parseBedrockCredentials(info.ApiKey)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	modelID := resolveBedrockModelID(info.ChannelId, info.UpstreamModelName, creds.region)
	if !isSonicModel(modelID) {
		return types.NewErrorWithStatusCode(errors.New("on AWS Bedrock the Realtime API serves the Amazon Nova Sonic models only"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	provider, err := sonicCredentials(c, creds)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	httpClient, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	options := bedrockruntime.Options{Region: creds.region, Credentials: provider, HTTPClient: httpClient}
	if info.ChannelBaseUrl != "" {
		options.BaseEndpoint = aws.String(strings.TrimRight(info.ChannelBaseUrl, "/"))
	}
	client := bedrockruntime.New(options)
	a.modelID = modelID
	a.sonicOpen = func(ctx context.Context) (sonicStream, error) {
		out, err := client.InvokeModelWithBidirectionalStream(ctx, &bedrockruntime.InvokeModelWithBidirectionalStreamInput{ModelId: aws.String(modelID)})
		if err != nil {
			return nil, err
		}
		return sdkSonicStream{stream: out.GetStream()}, nil
	}
	return nil
}

// sonicCredentials signs the stream with SigV4, which Bedrock requires of every event of a
// bidirectional stream: the channel's access key, or, for a channel keyed by a Bedrock API key
// on a managed gateway, the Gateway's own role.
func sonicCredentials(c *gin.Context, creds bedrockCredentials) (aws.CredentialsProvider, error) {
	if creds.apiKey == "" {
		return aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(creds.accessKey, creds.secretKey, "")), nil
	}
	if !settings.Managed() {
		return nil, errors.New("Amazon Nova Sonic needs a channel keyed by an access key: Bedrock API keys cannot sign a bidirectional stream")
	}
	return awsx.CredentialsProvider(c.Request.Context())
}

// serveSonic runs the Realtime session until the client or Bedrock ends it, and records the
// tokens Bedrock billed, whatever the outcome.
func (a *Adaptor) serveSonic(c *gin.Context, info *relaycommon.RelayInfo) (*dto.RealtimeUsage, *types.NewAPIError) {
	if info.ClientWs == nil || a.sonicOpen == nil {
		return nil, types.NewError(errors.New("invalid websocket connection"), types.ErrorCodeBadResponse)
	}
	session := newSonicSession(c, info, a.sonicOpen)
	apiErr := session.run()
	metering.Record(c, session.totals.units())
	return session.totals.realtimeUsage(), apiErr
}

type sonicTokens struct {
	SpeechTokens int64 `json:"speechTokens"`
	TextTokens   int64 `json:"textTokens"`
}

// sonicTotals are the session's tokens so far, as Bedrock's usage events report them.
type sonicTotals struct {
	Input  sonicTokens `json:"input"`
	Output sonicTokens `json:"output"`
}

// raise keeps the larger of each count: Bedrock's totals only grow, and an absurd count is not
// taken.
func (t *sonicTotals) raise(reported sonicTotals) {
	keep := func(current *int64, value int64) {
		if value > *current && value <= maxBedrockTokenCount {
			*current = value
		}
	}
	keep(&t.Input.SpeechTokens, reported.Input.SpeechTokens)
	keep(&t.Input.TextTokens, reported.Input.TextTokens)
	keep(&t.Output.SpeechTokens, reported.Output.SpeechTokens)
	keep(&t.Output.TextTokens, reported.Output.TextTokens)
}

func (t sonicTotals) units() metering.Units {
	return metering.Units{
		Input:        t.Input.TextTokens,
		Output:       t.Output.TextTokens,
		SpeechInput:  t.Input.SpeechTokens,
		SpeechOutput: t.Output.SpeechTokens,
	}
}

func (t sonicTotals) since(start sonicTotals) sonicTotals {
	return sonicTotals{
		Input:  sonicTokens{SpeechTokens: t.Input.SpeechTokens - start.Input.SpeechTokens, TextTokens: t.Input.TextTokens - start.Input.TextTokens},
		Output: sonicTokens{SpeechTokens: t.Output.SpeechTokens - start.Output.SpeechTokens, TextTokens: t.Output.TextTokens - start.Output.TextTokens},
	}
}

func (t sonicTotals) realtimeUsage() *dto.RealtimeUsage {
	input := int(t.Input.SpeechTokens + t.Input.TextTokens)
	output := int(t.Output.SpeechTokens + t.Output.TextTokens)
	usage := &dto.RealtimeUsage{TotalTokens: input + output, InputTokens: input, OutputTokens: output}
	usage.InputTokenDetails.TextTokens = int(t.Input.TextTokens)
	usage.InputTokenDetails.AudioTokens = int(t.Input.SpeechTokens)
	usage.OutputTokenDetails.TextTokens = int(t.Output.TextTokens)
	usage.OutputTokenDetails.AudioTokens = int(t.Output.SpeechTokens)
	return usage
}

// sonicConfig is what the client's session.update set before the session started.
type sonicConfig struct {
	Instructions string
	Voice        string
	Temperature  *float64
	MaxTokens    int
	Tools        []dto.RealTimeTool
}

type sonicContent struct {
	role, kind, stage string
}

// sonicTurn is the Realtime response being answered: Nova Sonic's turn of the assistant.
type sonicTurn struct {
	responseID string
	messageID  string
	transcript strings.Builder
	outputs    []map[string]any
	messageAt  int
}

// sonicSettling is an answer that has ended, its response.done waiting for Bedrock's usage.
type sonicSettling struct {
	responseID string
	status     string
	outputs    []map[string]any
	wait       *time.Timer
}

type sonicSession struct {
	info   *relaycommon.RelayInfo
	client *websocket.Conn
	open   sonicOpener
	model  string
	id     string

	ctx      context.Context
	cancel   context.CancelFunc
	upstream sonicStream
	writeMu  sync.Mutex
	done     chan struct{}
	failure  *types.NewAPIError
	// Bedrock's own failure, read once its events are done.
	upstreamErr *types.NewAPIError

	// Set by the client's events, before the session starts.
	config       sonicConfig
	audioContent string
	// Items sent before the first audio, passed on when it opens Bedrock's session.
	pending []func() *types.NewAPIError

	// Read and written while reading Bedrock's events, and by the wait for an answer's usage,
	// under stateMu; then read once Bedrock's events are done.
	stateMu  sync.Mutex
	contents map[string]sonicContent
	userText map[string]*strings.Builder
	turn     *sonicTurn
	settling *sonicSettling
	totals   sonicTotals
	// The totals the last response.done counted up to.
	answered sonicTotals
}

func newSonicSession(c *gin.Context, info *relaycommon.RelayInfo, open sonicOpener) *sonicSession {
	ctx, cancel := context.WithCancel(c.Request.Context())
	return &sonicSession{
		info:     info,
		client:   info.ClientWs,
		open:     open,
		model:    info.OriginModelName,
		id:       "sess_" + common.GetUUID(),
		ctx:      ctx,
		cancel:   cancel,
		config:   sonicConfig{Voice: sonicDefaultVoice, MaxTokens: sonicDefaultMaxTokens},
		contents: map[string]sonicContent{},
		userText: map[string]*strings.Builder{},
	}
}

// run serves the session. Its failure is never retried on another channel: the client already
// has the session.
func (s *sonicSession) run() *types.NewAPIError {
	defer s.cancel()
	s.client.SetReadLimit(sonicClientMessageLimit)
	s.emit(map[string]any{"type": "session.created", "session": s.sessionObject()})
	for {
		_, message, err := s.client.ReadMessage()
		if err != nil {
			break
		}
		if apiErr := s.clientEvent(message); apiErr != nil {
			s.failure = apiErr
			break
		}
	}
	s.finish()
	if s.failure == nil {
		s.failure = s.upstreamErr
	}
	if s.failure != nil {
		types.ErrOptionWithSkipRetry()(s.failure)
	}
	return s.failure
}

// finish ends Bedrock's session and waits for its last events, so every token it billed is
// counted.
func (s *sonicSession) finish() {
	if s.upstream == nil {
		return
	}
	if s.audioContent != "" {
		_ = s.send("contentEnd", map[string]any{"promptName": sonicPromptName, "contentName": s.audioContent})
	}
	_ = s.send("promptEnd", map[string]any{"promptName": sonicPromptName})
	_ = s.send("sessionEnd", map[string]any{})
	_ = s.upstream.CloseSend()
	select {
	case <-s.done:
	case <-time.After(sonicDrainTimeout):
		s.cancel()
		_ = s.upstream.Close()
		<-s.done
	}
}

func (s *sonicSession) emit(event map[string]any) {
	event["event_id"] = "event_" + common.GetUUID()
	body, err := common.Marshal(event)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.client.WriteMessage(websocket.TextMessage, body)
}

func (s *sonicSession) emitError(clientEventID, code, message string) {
	s.emit(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "code": code, "message": message, "event_id": clientEventID}})
}

func (s *sonicSession) send(name string, body map[string]any) error {
	event, err := common.Marshal(map[string]any{"event": map[string]any{name: body}})
	if err != nil {
		return err
	}
	return s.upstream.Send(s.ctx, event)
}

func (s *sonicSession) sessionObject() map[string]any {
	session := map[string]any{
		"id":                         s.id,
		"object":                     "realtime.session",
		"model":                      s.model,
		"modalities":                 []string{"audio", "text"},
		"instructions":               s.config.Instructions,
		"voice":                      s.config.Voice,
		"input_audio_format":         "pcm16",
		"output_audio_format":        "pcm16",
		"input_audio_transcription":  map[string]any{"model": s.model},
		"turn_detection":             map[string]any{"type": "server_vad"},
		"tools":                      s.config.Tools,
		"tool_choice":                "auto",
		"max_response_output_tokens": s.config.MaxTokens,
	}
	if s.config.Tools == nil {
		session["tools"] = []dto.RealTimeTool{}
	}
	if s.config.Temperature != nil {
		session["temperature"] = *s.config.Temperature
	}
	return session
}

type sonicClientItem struct {
	Type    string                `json:"type"`
	Role    string                `json:"role"`
	CallID  string                `json:"call_id"`
	Output  string                `json:"output"`
	Content []dto.RealtimeContent `json:"content"`
}

type sonicSessionUpdate struct {
	Instructions            *string             `json:"instructions"`
	Voice                   *string             `json:"voice"`
	InputAudioFormat        *string             `json:"input_audio_format"`
	OutputAudioFormat       *string             `json:"output_audio_format"`
	Temperature             *float64            `json:"temperature"`
	MaxResponseOutputTokens any                 `json:"max_response_output_tokens"`
	Tools                   *[]dto.RealTimeTool `json:"tools"`
}

// clientEvent passes one Realtime client event on to Bedrock. A request the session cannot
// serve is answered with an error event and the session goes on; a Bedrock failure ends it.
func (s *sonicSession) clientEvent(message []byte) *types.NewAPIError {
	var event struct {
		EventID string              `json:"event_id"`
		Type    string              `json:"type"`
		Session *sonicSessionUpdate `json:"session"`
		Audio   string              `json:"audio"`
		Item    *sonicClientItem    `json:"item"`
	}
	if err := common.Unmarshal(message, &event); err != nil {
		s.emitError("", "invalid_json", "The event is not valid JSON.")
		return nil
	}
	switch event.Type {
	case "session.update":
		if problem := s.updateSession(event.Session); problem != "" {
			s.emitError(event.EventID, "invalid_value", problem)
			return nil
		}
		s.emit(map[string]any{"type": "session.updated", "session": s.sessionObject()})
		return nil
	case "input_audio_buffer.append":
		audio, err := base64.StdEncoding.DecodeString(event.Audio)
		if err != nil || len(audio) == 0 {
			s.emitError(event.EventID, "invalid_value", "audio must be base64-encoded pcm16.")
			return nil
		}
		return s.appendAudio(audio)
	case "input_audio_buffer.commit":
		s.emit(map[string]any{"type": "input_audio_buffer.committed", "item_id": "item_" + common.GetUUID()})
		return nil
	case "input_audio_buffer.clear":
		s.emit(map[string]any{"type": "input_audio_buffer.cleared"})
		return nil
	case "conversation.item.create":
		if event.Item == nil {
			s.emitError(event.EventID, "missing_required_parameter", "item is required.")
			return nil
		}
		return s.createItem(event.EventID, *event.Item)
	case "response.create":
		if s.upstream == nil {
			s.emitError(event.EventID, "audio_required", sonicAudioRequired)
		}
		return nil
	case "response.cancel":
		return nil
	}
	s.emitError(event.EventID, "unsupported_event", "Amazon Nova Sonic does not support "+event.Type+".")
	return nil
}

// updateSession applies a session.update; it is "" when the update was applied, or what is wrong.
func (s *sonicSession) updateSession(update *sonicSessionUpdate) string {
	if update == nil {
		return "session is required."
	}
	if s.upstream != nil {
		return "Amazon Nova Sonic sets up the session when it starts: send session.update before the first audio."
	}
	for _, format := range []*string{update.InputAudioFormat, update.OutputAudioFormat} {
		if format != nil && *format != "pcm16" {
			return "Amazon Nova Sonic takes and gives pcm16 audio only."
		}
	}
	config := s.config
	switch tokens := update.MaxResponseOutputTokens.(type) {
	case nil:
	case string:
		if tokens != "inf" {
			return "max_response_output_tokens must be a number or \"inf\"."
		}
		config.MaxTokens = sonicMaxTokensLimit
	case float64:
		if tokens < 1 || tokens > sonicMaxTokensLimit || tokens != float64(int(tokens)) {
			return "max_response_output_tokens must be a whole number from 1 to 10000."
		}
		config.MaxTokens = int(tokens)
	default:
		return "max_response_output_tokens must be a number or \"inf\"."
	}
	if update.Temperature != nil {
		if *update.Temperature < 0 || *update.Temperature > 1 {
			return "temperature must be between 0 and 1."
		}
		config.Temperature = update.Temperature
	}
	if update.Instructions != nil {
		config.Instructions = *update.Instructions
	}
	if update.Voice != nil {
		config.Voice = strings.ToLower(strings.TrimSpace(*update.Voice))
		if config.Voice == "" || slices.Contains(openAIVoices, config.Voice) {
			config.Voice = sonicDefaultVoice
		}
	}
	if update.Tools != nil {
		config.Tools = *update.Tools
	}
	s.config = config
	return ""
}

// afterAudio sends an item to Bedrock, or keeps it until the first audio opens the session:
// Nova Sonic answers speech only.
func (s *sonicSession) afterAudio(send func() *types.NewAPIError) *types.NewAPIError {
	if s.upstream == nil {
		s.pending = append(s.pending, send)
		return nil
	}
	return send()
}

// start opens Bedrock's stream and its session with the client's configuration, once.
func (s *sonicSession) start() *types.NewAPIError {
	if s.upstream != nil {
		return nil
	}
	stream, err := s.open(s.ctx)
	if err != nil {
		return newAwsInvokeError(s.ctx, err, "InvokeModelWithBidirectionalStream")
	}
	s.upstream = stream
	s.done = make(chan struct{})
	go s.readUpstream()

	inference := map[string]any{"maxTokens": s.config.MaxTokens}
	if s.config.Temperature != nil {
		inference["temperature"] = *s.config.Temperature
	}
	prompt := map[string]any{
		"promptName":              sonicPromptName,
		"textOutputConfiguration": map[string]any{"mediaType": "text/plain"},
		"audioOutputConfiguration": map[string]any{
			"mediaType": "audio/lpcm", "sampleRateHertz": sonicSampleRate, "sampleSizeBits": 16, "channelCount": 1,
			"voiceId": s.config.Voice, "encoding": "base64", "audioType": "SPEECH",
		},
	}
	if len(s.config.Tools) > 0 {
		tools := make([]map[string]any, 0, len(s.config.Tools))
		for _, tool := range s.config.Tools {
			schema, err := common.Marshal(tool.Parameters)
			if err != nil || tool.Parameters == nil {
				schema = []byte(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{"toolSpec": map[string]any{
				"name": tool.Name, "description": tool.Description, "inputSchema": map[string]any{"json": string(schema)},
			}})
		}
		prompt["toolUseOutputConfiguration"] = map[string]any{"mediaType": "application/json"}
		prompt["toolConfiguration"] = map[string]any{"tools": tools}
	}
	if err := s.send("sessionStart", map[string]any{"inferenceConfiguration": inference}); err != nil {
		return s.upstreamFailure(err)
	}
	if err := s.send("promptStart", prompt); err != nil {
		return s.upstreamFailure(err)
	}
	if s.config.Instructions != "" {
		return s.sendText("SYSTEM", s.config.Instructions, false)
	}
	return nil
}

func (s *sonicSession) upstreamFailure(err error) *types.NewAPIError {
	return newAwsInvokeError(s.ctx, err, "InvokeModelWithBidirectionalStream")
}

func (s *sonicSession) sendText(role, text string, interactive bool) *types.NewAPIError {
	name := "text_" + common.GetUUID()
	start := map[string]any{
		"promptName": sonicPromptName, "contentName": name, "type": "TEXT", "interactive": interactive, "role": role,
		"textInputConfiguration": map[string]any{"mediaType": "text/plain"},
	}
	for _, event := range []struct {
		name string
		body map[string]any
	}{
		{"contentStart", start},
		{"textInput", map[string]any{"promptName": sonicPromptName, "contentName": name, "content": text}},
		{"contentEnd", map[string]any{"promptName": sonicPromptName, "contentName": name}},
	} {
		if err := s.send(event.name, event.body); err != nil {
			return s.upstreamFailure(err)
		}
	}
	return nil
}

func (s *sonicSession) appendAudio(audio []byte) *types.NewAPIError {
	if s.upstream == nil {
		if apiErr := s.start(); apiErr != nil {
			return apiErr
		}
		for _, send := range s.pending {
			if apiErr := send(); apiErr != nil {
				return apiErr
			}
		}
		s.pending = nil
	}
	if s.audioContent == "" {
		s.audioContent = "audio_" + common.GetUUID()
		err := s.send("contentStart", map[string]any{
			"promptName": sonicPromptName, "contentName": s.audioContent, "type": "AUDIO", "interactive": true, "role": "USER",
			"audioInputConfiguration": map[string]any{
				"mediaType": "audio/lpcm", "sampleRateHertz": sonicSampleRate, "sampleSizeBits": 16, "channelCount": 1,
				"audioType": "SPEECH", "encoding": "base64",
			},
		})
		if err != nil {
			return s.upstreamFailure(err)
		}
	}
	for start := 0; start < len(audio); start += sonicAudioChunk {
		chunk := audio[start:min(start+sonicAudioChunk, len(audio))]
		err := s.send("audioInput", map[string]any{"promptName": sonicPromptName, "contentName": s.audioContent, "content": base64.StdEncoding.EncodeToString(chunk)})
		if err != nil {
			return s.upstreamFailure(err)
		}
	}
	return nil
}

// createItem passes on a conversation item: a person's text, which Nova Sonic answers with the
// audio it accompanies, an earlier answer of the assistant as history, or a function call's output.
func (s *sonicSession) createItem(clientEventID string, item sonicClientItem) *types.NewAPIError {
	itemID := "item_" + common.GetUUID()
	switch {
	case item.Type == "function_call_output":
		if item.CallID == "" {
			s.emitError(clientEventID, "missing_required_parameter", "call_id is required.")
			return nil
		}
		if apiErr := s.afterAudio(func() *types.NewAPIError { return s.sendToolResult(item.CallID, item.Output) }); apiErr != nil {
			return apiErr
		}
		s.emit(map[string]any{"type": "conversation.item.created", "item": map[string]any{
			"id": itemID, "object": "realtime.item", "type": "function_call_output", "call_id": item.CallID, "output": item.Output,
		}})
		return nil
	case item.Type == "message" && (item.Role == "user" || item.Role == "assistant"):
		var text strings.Builder
		for _, content := range item.Content {
			if content.Type != "input_text" && content.Type != "text" {
				s.emitError(clientEventID, "invalid_value", "Amazon Nova Sonic takes text items; send audio with input_audio_buffer.append.")
				return nil
			}
			text.WriteString(content.Text)
		}
		if text.Len() == 0 {
			s.emitError(clientEventID, "invalid_value", "The item has no text.")
			return nil
		}
		role, interactive, contentType := "USER", true, "input_text"
		if item.Role == "assistant" {
			role, interactive, contentType = "ASSISTANT", false, "text"
		}
		if apiErr := s.afterAudio(func() *types.NewAPIError { return s.sendText(role, text.String(), interactive) }); apiErr != nil {
			return apiErr
		}
		s.emit(map[string]any{"type": "conversation.item.created", "item": map[string]any{
			"id": itemID, "object": "realtime.item", "type": "message", "status": "completed", "role": item.Role,
			"content": []map[string]any{{"type": contentType, "text": text.String()}},
		}})
		return nil
	}
	s.emitError(clientEventID, "invalid_value", "Amazon Nova Sonic takes user and assistant text messages and function call outputs.")
	return nil
}

func (s *sonicSession) sendToolResult(callID, output string) *types.NewAPIError {
	name := "tool_" + common.GetUUID()
	events := []struct {
		name string
		body map[string]any
	}{
		{"contentStart", map[string]any{
			"promptName": sonicPromptName, "contentName": name, "interactive": false, "type": "TOOL", "role": "TOOL",
			"toolResultInputConfiguration": map[string]any{"toolUseId": callID, "type": "TEXT", "textInputConfiguration": map[string]any{"mediaType": "text/plain"}},
		}},
		{"toolResult", map[string]any{"promptName": sonicPromptName, "contentName": name, "content": output}},
		{"contentEnd", map[string]any{"promptName": sonicPromptName, "contentName": name}},
	}
	for _, event := range events {
		if err := s.send(event.name, event.body); err != nil {
			return s.upstreamFailure(err)
		}
	}
	return nil
}

type sonicOutputEvent struct {
	Event struct {
		ContentStart *struct {
			ContentID             string `json:"contentId"`
			Role                  string `json:"role"`
			Type                  string `json:"type"`
			AdditionalModelFields string `json:"additionalModelFields"`
		} `json:"contentStart"`
		TextOutput *struct {
			ContentID string `json:"contentId"`
			Content   string `json:"content"`
		} `json:"textOutput"`
		AudioOutput *struct {
			Content string `json:"content"`
		} `json:"audioOutput"`
		ToolUse *struct {
			ToolName  string `json:"toolName"`
			ToolUseID string `json:"toolUseId"`
			Content   string `json:"content"`
		} `json:"toolUse"`
		ContentEnd *struct {
			ContentID  string `json:"contentId"`
			StopReason string `json:"stopReason"`
		} `json:"contentEnd"`
		UsageEvent *struct {
			Details struct {
				Total sonicTotals `json:"total"`
			} `json:"details"`
		} `json:"usageEvent"`
		CompletionEnd   *struct{} `json:"completionEnd"`
		UserSpeechStart *struct {
			InputAudioOffsetMs int64 `json:"inputAudioOffsetMs"`
		} `json:"userSpeechStart"`
		UserSpeechEnd *struct {
			InputAudioOffsetMs int64 `json:"inputAudioOffsetMs"`
		} `json:"userSpeechEnd"`
	} `json:"event"`
}

// readUpstream turns Bedrock's events into Realtime server events until the stream ends. When
// Bedrock ends it with an error, the client's reading is stopped so the session ends too.
func (s *sonicSession) readUpstream() {
	defer close(s.done)
	for {
		message, ok := s.upstream.Receive()
		if !ok {
			break
		}
		var event sonicOutputEvent
		if err := common.Unmarshal(message, &event); err != nil {
			continue
		}
		s.stateMu.Lock()
		s.upstreamEvent(event)
		s.stateMu.Unlock()
	}
	s.stateMu.Lock()
	s.endTurn("incomplete")
	s.answerDone()
	s.stateMu.Unlock()
	if err := s.upstream.Err(); err != nil && s.ctx.Err() == nil {
		s.upstreamErr = s.upstreamFailure(err)
		_ = s.client.SetReadDeadline(time.Now())
	}
}

func (s *sonicSession) upstreamEvent(event sonicOutputEvent) {
	e := event.Event
	switch {
	case e.UsageEvent != nil:
		s.totals.raise(e.UsageEvent.Details.Total)
		s.answerDone()
	case e.ContentStart != nil:
		var fields struct {
			GenerationStage string `json:"generationStage"`
		}
		_ = common.UnmarshalJsonStr(e.ContentStart.AdditionalModelFields, &fields)
		content := sonicContent{role: e.ContentStart.Role, kind: e.ContentStart.Type, stage: fields.GenerationStage}
		s.contents[e.ContentStart.ContentID] = content
		if content.role == "USER" && content.kind == "TEXT" {
			s.userText[e.ContentStart.ContentID] = &strings.Builder{}
		}
		if content.role == "ASSISTANT" && content.stage != "FINAL" {
			s.beginTurn()
		}
	case e.TextOutput != nil:
		content := s.contents[e.TextOutput.ContentID]
		if builder, ok := s.userText[e.TextOutput.ContentID]; ok {
			builder.WriteString(e.TextOutput.Content)
			return
		}
		if content.role != "ASSISTANT" || content.stage != "FINAL" || s.turn == nil || sonicInterruption(e.TextOutput.Content) {
			return
		}
		s.openMessage()
		s.turn.transcript.WriteString(e.TextOutput.Content)
		s.emit(s.turn.part("response.audio_transcript.delta", map[string]any{"delta": e.TextOutput.Content}))
	case e.AudioOutput != nil:
		s.beginTurn()
		s.openMessage()
		s.info.SetFirstResponseTime()
		s.emit(s.turn.part("response.audio.delta", map[string]any{"delta": e.AudioOutput.Content}))
	case e.ToolUse != nil:
		s.beginTurn()
		s.functionCall(e.ToolUse.ToolUseID, e.ToolUse.ToolName, e.ToolUse.Content)
	case e.ContentEnd != nil:
		s.contentEnd(e.ContentEnd.ContentID, e.ContentEnd.StopReason)
	case e.CompletionEnd != nil:
		s.endTurn("completed")
	case e.UserSpeechStart != nil:
		s.emit(map[string]any{"type": "input_audio_buffer.speech_started", "audio_start_ms": e.UserSpeechStart.InputAudioOffsetMs, "item_id": ""})
	case e.UserSpeechEnd != nil:
		s.emit(map[string]any{"type": "input_audio_buffer.speech_stopped", "audio_end_ms": e.UserSpeechEnd.InputAudioOffsetMs, "item_id": ""})
	}
}

// sonicInterruption tells the text Nova Sonic sends when the person spoke over its answer.
func sonicInterruption(text string) bool {
	var marker struct {
		Interrupted bool `json:"interrupted"`
	}
	return common.UnmarshalJsonStr(strings.TrimSpace(text), &marker) == nil && marker.Interrupted
}

func (s *sonicSession) contentEnd(contentID, stopReason string) {
	content := s.contents[contentID]
	delete(s.contents, contentID)
	if builder, ok := s.userText[contentID]; ok {
		delete(s.userText, contentID)
		if transcript := builder.String(); transcript != "" {
			itemID := "item_" + common.GetUUID()
			s.emit(map[string]any{"type": "conversation.item.created", "item": map[string]any{
				"id": itemID, "object": "realtime.item", "type": "message", "status": "completed", "role": "user",
				"content": []map[string]any{{"type": "input_audio", "transcript": transcript}},
			}})
			s.emit(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": itemID, "content_index": 0, "transcript": transcript})
		}
		return
	}
	switch {
	case stopReason == "INTERRUPTED" && content.role == "ASSISTANT":
		s.endTurn("cancelled")
	case stopReason == "TOOL_USE":
		s.endTurn("completed")
	case stopReason == "END_TURN" && content.role == "ASSISTANT" && content.kind == "TEXT" && content.stage == "FINAL":
		s.endTurn("completed")
	}
}

func (s *sonicSession) beginTurn() {
	if s.turn != nil {
		return
	}
	s.answerDone()
	s.turn = &sonicTurn{responseID: "resp_" + common.GetUUID(), messageAt: -1}
	s.emit(map[string]any{"type": "response.created", "response": map[string]any{
		"id": s.turn.responseID, "object": "realtime.response", "status": "in_progress", "output": []any{},
	}})
}

// part is a server event about the audio part of the turn's message.
func (t *sonicTurn) part(eventType string, fields map[string]any) map[string]any {
	fields["type"] = eventType
	fields["response_id"] = t.responseID
	fields["item_id"] = t.messageID
	fields["output_index"] = t.messageAt
	fields["content_index"] = 0
	return fields
}

func (s *sonicSession) openMessage() {
	if s.turn.messageAt >= 0 {
		return
	}
	s.turn.messageID = "item_" + common.GetUUID()
	s.turn.messageAt = len(s.turn.outputs)
	s.turn.outputs = append(s.turn.outputs, nil)
	s.emit(map[string]any{"type": "response.output_item.added", "response_id": s.turn.responseID, "output_index": s.turn.messageAt, "item": map[string]any{
		"id": s.turn.messageID, "object": "realtime.item", "type": "message", "status": "in_progress", "role": "assistant", "content": []any{},
	}})
	s.emit(s.turn.part("response.content_part.added", map[string]any{"part": map[string]any{"type": "audio", "transcript": ""}}))
}

func (s *sonicSession) functionCall(callID, name, arguments string) {
	turn := s.turn
	index := len(turn.outputs)
	itemID := "item_" + common.GetUUID()
	item := map[string]any{"id": itemID, "object": "realtime.item", "type": "function_call", "status": "in_progress", "call_id": callID, "name": name, "arguments": ""}
	s.emit(map[string]any{"type": "response.output_item.added", "response_id": turn.responseID, "output_index": index, "item": item})
	s.emit(map[string]any{"type": "response.function_call_arguments.delta", "response_id": turn.responseID, "item_id": itemID, "output_index": index, "call_id": callID, "delta": arguments})
	s.emit(map[string]any{"type": "response.function_call_arguments.done", "response_id": turn.responseID, "item_id": itemID, "output_index": index, "call_id": callID, "name": name, "arguments": arguments})
	done := map[string]any{"id": itemID, "object": "realtime.item", "type": "function_call", "status": "completed", "call_id": callID, "name": name, "arguments": arguments}
	s.emit(map[string]any{"type": "response.output_item.done", "response_id": turn.responseID, "output_index": index, "item": done})
	turn.outputs = append(turn.outputs, done)
}

func (s *sonicSession) endTurn(status string) {
	turn := s.turn
	if turn == nil {
		return
	}
	s.turn = nil
	if turn.messageAt >= 0 {
		transcript := turn.transcript.String()
		part := map[string]any{"type": "audio", "transcript": transcript}
		s.emit(turn.part("response.audio.done", map[string]any{}))
		s.emit(turn.part("response.audio_transcript.done", map[string]any{"transcript": transcript}))
		s.emit(turn.part("response.content_part.done", map[string]any{"part": part}))
		message := map[string]any{"id": turn.messageID, "object": "realtime.item", "type": "message", "status": "completed", "role": "assistant", "content": []any{part}}
		if status != "completed" {
			message["status"] = "incomplete"
		}
		s.emit(map[string]any{"type": "response.output_item.done", "response_id": turn.responseID, "output_index": turn.messageAt, "item": message})
		turn.outputs[turn.messageAt] = message
	}
	s.answerDone()
	settling := &sonicSettling{responseID: turn.responseID, status: status, outputs: turn.outputs}
	settling.wait = time.AfterFunc(sonicTurnUsageWait, func() {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
		if s.settling == settling {
			s.answerDone()
		}
	})
	s.settling = settling
}

// answerDone sends the response.done of the answer that ended, once Bedrock reported its usage,
// the next answer began, the wait ran out or the session ended. Its usage is every token
// Bedrock reported since the previous answer's: the person's speech and text that led to it,
// and the answer. Tokens reported later count in the next answer; together the answers count
// what the session is billed.
func (s *sonicSession) answerDone() {
	settling := s.settling
	if settling == nil {
		return
	}
	s.settling = nil
	settling.wait.Stop()
	usage := s.totals.since(s.answered).realtimeUsage()
	s.answered = s.totals
	s.emit(map[string]any{"type": "response.done", "response": map[string]any{
		"id": settling.responseID, "object": "realtime.response", "status": settling.status, "output": settling.outputs,
		"usage": usage,
	}})
}
