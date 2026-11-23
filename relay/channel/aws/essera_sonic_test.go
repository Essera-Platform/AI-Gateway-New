// Essera fork: Nova Sonic through the Realtime API, against a scripted Bedrock stream whose
// events have the shape Bedrock sent in us-east-1.
package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/metering"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/aws/smithy-go"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSonicStream struct {
	mu        sync.Mutex
	sent      []map[string]map[string]any
	events    chan []byte
	closeOnce sync.Once
	err       error
}

func newFakeSonicStream(events ...string) *fakeSonicStream {
	stream := &fakeSonicStream{events: make(chan []byte, len(events))}
	for _, event := range events {
		stream.events <- []byte(event)
	}
	return stream
}

func (f *fakeSonicStream) Send(_ context.Context, event []byte) error {
	var decoded struct {
		Event map[string]map[string]any `json:"event"`
	}
	if err := common.Unmarshal(event, &decoded); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, decoded.Event)
	return nil
}

func (f *fakeSonicStream) Receive() ([]byte, bool) {
	event, ok := <-f.events
	return event, ok
}

// CloseSend ends the stream as Bedrock does once the session has ended.
func (f *fakeSonicStream) CloseSend() error {
	f.closeOnce.Do(func() { close(f.events) })
	return nil
}

func (f *fakeSonicStream) Close() error { return f.CloseSend() }
func (f *fakeSonicStream) Err() error   { return f.err }

// sentEvents are the names and bodies of the events sent to Bedrock, in order.
func (f *fakeSonicStream) sentEvents() []sentSonicEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var events []sentSonicEvent
	for _, event := range f.sent {
		for name, body := range event {
			events = append(events, sentSonicEvent{name, body})
		}
	}
	return events
}

type sentSonicEvent struct {
	name string
	body map[string]any
}

type sonicOutcome struct {
	units  metering.Units
	apiErr *relaytypes.NewAPIError
}

type sonicClient struct {
	t      *testing.T
	conn   *websocket.Conn
	result chan sonicOutcome
}

func startSonicSession(t *testing.T, stream *fakeSonicStream) *sonicClient {
	t.Helper()
	gin.SetMode(gin.TestMode)
	result := make(chan sonicOutcome, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		info := &relaycommon.RelayInfo{ClientWs: conn, StartTime: time.Now()}
		info.OriginModelName = "amazon.nova-2-sonic-v1:0"
		adaptor := &Adaptor{sonicOpen: func(context.Context) (sonicStream, error) { return stream, nil }}
		_, apiErr := adaptor.serveSonic(c, info)
		units, _ := metering.Recorded(c)
		result <- sonicOutcome{units: units, apiErr: apiErr}
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	return &sonicClient{t: t, conn: conn, result: result}
}

func (s *sonicClient) send(event map[string]any) {
	body, err := common.Marshal(event)
	require.NoError(s.t, err)
	require.NoError(s.t, s.conn.WriteMessage(websocket.TextMessage, body))
}

// until reads server events up to the first of the given type, and returns them all.
func (s *sonicClient) until(eventType string) []map[string]any {
	var events []map[string]any
	for {
		require.NoError(s.t, s.conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, message, err := s.conn.ReadMessage()
		require.NoError(s.t, err, "waiting for %s", eventType)
		var event map[string]any
		require.NoError(s.t, common.Unmarshal(message, &event))
		events = append(events, event)
		if event["type"] == eventType {
			return events
		}
	}
}

func (s *sonicClient) close() sonicOutcome {
	_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = s.conn.Close()
	select {
	case outcome := <-s.result:
		return outcome
	case <-time.After(5 * time.Second):
		s.t.Fatal("the session did not end")
		return sonicOutcome{}
	}
}

func typesOf(events []map[string]any) []string {
	names := make([]string, len(events))
	for i, event := range events {
		names[i] = event["type"].(string)
	}
	return names
}

func sonicUsage(inSpeech, inText, outSpeech, outText int) string {
	return `{"event":{"usageEvent":{"details":{"total":{"input":{"speechTokens":` + itoa(inSpeech) + `,"textTokens":` + itoa(inText) + `},"output":{"speechTokens":` + itoa(outSpeech) + `,"textTokens":` + itoa(outText) + `}}}}}}`
}

func itoa(n int) string {
	body, _ := common.Marshal(n)
	return string(body)
}

func sonicContentStart(id, role, kind, stage string) string {
	fields := ""
	if stage != "" {
		fields = `,"additionalModelFields":"{\"generationStage\":\"` + stage + `\"}"`
	}
	return `{"event":{"contentStart":{"contentId":"` + id + `","role":"` + role + `","type":"` + kind + `"` + fields + `}}}`
}

func sonicText(id, text string) string {
	return `{"event":{"textOutput":{"contentId":"` + id + `","content":"` + text + `"}}}`
}

func sonicContentEnd(id, stopReason string) string {
	return `{"event":{"contentEnd":{"contentId":"` + id + `","stopReason":"` + stopReason + `"}}}`
}

func TestSonicSpeechInSpeechAndTranscriptOut(t *testing.T) {
	stream := newFakeSonicStream(
		sonicUsage(150, 22, 0, 0),
		`{"event":{"userSpeechStart":{"inputAudioOffsetMs":0}}}`,
		sonicContentStart("u1", "USER", "TEXT", "FINAL"),
		sonicText("u1", "what is two plus two"),
		sonicContentEnd("u1", "PARTIAL_TURN"),
		sonicContentStart("a1", "ASSISTANT", "TEXT", "SPECULATIVE"),
		sonicText("a1", "Four."),
		sonicContentEnd("a1", "PARTIAL_TURN"),
		sonicContentStart("a2", "ASSISTANT", "AUDIO", ""),
		`{"event":{"audioOutput":{"contentId":"a2","content":"AAAA"}}}`,
		sonicContentEnd("a2", "END_TURN"),
		sonicContentStart("a3", "ASSISTANT", "TEXT", "FINAL"),
		sonicText("a3", "Four."),
		sonicContentEnd("a3", "END_TURN"),
		sonicUsage(264, 351, 33, 32),
	)
	client := startSonicSession(t, stream)
	client.until("session.created")
	client.send(map[string]any{"type": "session.update", "session": map[string]any{"instructions": "Be brief.", "voice": "alloy"}})
	client.until("session.updated")
	audio := make([]byte, sonicAudioChunk+8000)
	client.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(audio)})

	events := client.until("response.done")
	assert.Equal(t, []string{
		"input_audio_buffer.speech_started",
		"conversation.item.created",
		"conversation.item.input_audio_transcription.completed",
		"response.created",
		"response.output_item.added",
		"response.content_part.added",
		"response.audio.delta",
		"response.audio_transcript.delta",
		"response.audio.done",
		"response.audio_transcript.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.done",
	}, typesOf(events))
	assert.Equal(t, "what is two plus two", events[2]["transcript"])
	assert.Equal(t, "AAAA", events[6]["delta"])
	assert.Equal(t, "Four.", events[9]["transcript"], "the final text, said once")
	assert.Equal(t, "completed", events[12]["response"].(map[string]any)["status"])
	usage := events[12]["response"].(map[string]any)["usage"].(map[string]any)
	inputDetails := usage["input_token_details"].(map[string]any)
	outputDetails := usage["output_token_details"].(map[string]any)
	assert.Equal(t, []float64{615, 351, 264}, []float64{usage["input_tokens"].(float64), inputDetails["text_tokens"].(float64), inputDetails["audio_tokens"].(float64)},
		"the answer waits for Bedrock's usage of it, and counts the instructions and the person's speech that led to it")
	assert.Equal(t, []float64{65, 32, 33}, []float64{usage["output_tokens"].(float64), outputDetails["text_tokens"].(float64), outputDetails["audio_tokens"].(float64)})

	outcome := client.close()
	require.Nil(t, outcome.apiErr)
	assert.Equal(t, metering.Units{Input: 351, Output: 32, SpeechInput: 264, SpeechOutput: 33}, outcome.units, "the session's last totals, speech and text apart")

	sent := stream.sentEvents()
	var names []string
	for _, event := range sent {
		names = append(names, event.name)
	}
	assert.Equal(t, []string{
		"sessionStart", "promptStart",
		"contentStart", "textInput", "contentEnd",
		"contentStart", "audioInput", "audioInput",
		"contentEnd", "promptEnd", "sessionEnd",
	}, names)
	prompt := sent[1].body
	assert.Equal(t, "matthew", prompt["audioOutputConfiguration"].(map[string]any)["voiceId"], "an OpenAI voice is Nova Sonic's default")
	assert.Equal(t, "SYSTEM", sent[2].body["role"])
	assert.Equal(t, "Be brief.", sent[3].body["content"])
	audioStart := sent[5].body
	assert.Equal(t, "AUDIO", audioStart["type"])
	assert.EqualValues(t, sonicSampleRate, audioStart["audioInputConfiguration"].(map[string]any)["sampleRateHertz"])
	first, err := base64.StdEncoding.DecodeString(sent[6].body["content"].(string))
	require.NoError(t, err)
	second, err := base64.StdEncoding.DecodeString(sent[7].body["content"].(string))
	require.NoError(t, err)
	assert.Equal(t, []int{sonicAudioChunk, 8000}, []int{len(first), len(second)})
	assert.Equal(t, audioStart["contentName"], sent[8].body["contentName"], "the audio is ended before the prompt")
}

func TestSonicFunctionCall(t *testing.T) {
	stream := newFakeSonicStream(
		sonicContentStart("t1", "TOOL", "TOOL", ""),
		`{"event":{"toolUse":{"contentId":"t1","toolName":"get_weather","toolUseId":"call-1","content":"{\"city\":\"Paris\"}"}}}`,
		sonicContentEnd("t1", "TOOL_USE"),
	)
	client := startSonicSession(t, stream)
	client.until("session.created")
	client.send(map[string]any{"type": "session.update", "session": map[string]any{"tools": []any{map[string]any{
		"type": "function", "name": "get_weather", "description": "Weather in a city",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
	}}}})
	client.until("session.updated")
	client.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 480))})

	events := client.until("response.done")
	assert.Equal(t, []string{
		"response.created",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.done",
	}, typesOf(events))
	done := events[3]
	assert.Equal(t, "call-1", done["call_id"])
	assert.Equal(t, "get_weather", done["name"])
	assert.Equal(t, `{"city":"Paris"}`, done["arguments"])

	client.send(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": "call-1", "output": `{"sky":"cloudy"}`}})
	client.until("conversation.item.created")
	require.Nil(t, client.close().apiErr)

	sent := stream.sentEvents()
	tools := sent[1].body["toolConfiguration"].(map[string]any)["tools"].([]any)
	spec := tools[0].(map[string]any)["toolSpec"].(map[string]any)
	assert.Equal(t, "get_weather", spec["name"])
	assert.JSONEq(t, `{"type":"object","properties":{"city":{"type":"string"}}}`, spec["inputSchema"].(map[string]any)["json"].(string))
	var result []sentSonicEvent
	for _, event := range sent {
		if event.body["role"] == "TOOL" || event.name == "toolResult" {
			result = append(result, event)
		}
	}
	require.Len(t, result, 2)
	assert.Equal(t, "call-1", result[0].body["toolResultInputConfiguration"].(map[string]any)["toolUseId"])
	assert.Equal(t, `{"sky":"cloudy"}`, result[1].body["content"])
}

func TestSonicSessionUpdate(t *testing.T) {
	cases := []struct {
		name    string
		session map[string]any
		refused bool
	}{
		{"pcm16 both ways", map[string]any{"input_audio_format": "pcm16", "output_audio_format": "pcm16"}, false},
		{"g711 audio", map[string]any{"input_audio_format": "g711_ulaw"}, true},
		{"answer tokens in range", map[string]any{"max_response_output_tokens": 2048}, false},
		{"answer tokens unbounded", map[string]any{"max_response_output_tokens": "inf"}, false},
		{"answer tokens above the limit", map[string]any{"max_response_output_tokens": sonicMaxTokensLimit + 1}, true},
		{"answer tokens fractional", map[string]any{"max_response_output_tokens": 10.5}, true},
		{"temperature above 1", map[string]any{"temperature": 1.5}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := newSonicSession(&gin.Context{Request: httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)}, &relaycommon.RelayInfo{}, nil)
			var update sonicSessionUpdate
			body, err := common.Marshal(tc.session)
			require.NoError(t, err)
			require.NoError(t, common.Unmarshal(body, &update))
			assert.Equal(t, tc.refused, session.updateSession(&update) != "")
		})
	}

	stream := newFakeSonicStream()
	client := startSonicSession(t, stream)
	client.until("session.created")
	client.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 480))})
	client.send(map[string]any{"type": "session.update", "session": map[string]any{"voice": "tiffany"}})
	refusal := client.until("error")
	assert.Contains(t, refusal[len(refusal)-1]["error"].(map[string]any)["message"], "before the first audio")
	require.Nil(t, client.close().apiErr)
}

func TestSonicBedrockFailureEndsTheSessionWithItsTokens(t *testing.T) {
	stream := newFakeSonicStream(sonicUsage(100, 20, 0, 0))
	stream.err = &smithy.GenericAPIError{Code: "ModelStreamErrorException", Message: "stream failed"}
	client := startSonicSession(t, stream)
	client.until("session.created")
	client.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 480))})
	require.NoError(t, stream.CloseSend())

	var outcome sonicOutcome
	select {
	case outcome = <-client.result:
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not end")
	}
	require.NotNil(t, outcome.apiErr)
	assert.Equal(t, http.StatusBadGateway, outcome.apiErr.StatusCode)
	assert.True(t, relaytypes.IsSkipRetryError(outcome.apiErr), "the client already has part of the session")
	assert.Equal(t, metering.Units{Input: 20, SpeechInput: 100}, outcome.units)
}

func TestSonicRefusesOtherModels(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "AKID|SECRET|us-east-1", UpstreamModelName: "amazon.nova-lite-v1:0"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	err := adaptor.prepareSonic(c, info)
	var apiErr *relaytypes.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}
