package usage

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
)

func TestGeneratedUnitsAreWhatTheConsolePrices(t *testing.T) {
	two := 2
	cases := []struct {
		name                     string
		event                    Event
		info                     relaycommon.RelayInfo
		metered                  bool
		input, output, generated int64
	}{
		{"images settled, with new-api's placeholder token cleared", Event{Modality: "image", InputUnits: 1},
			relaycommon.RelayInfo{BillingImageCount: &two}, false, 0, 0, 2},
		{"images the provider billed, metered by the adaptor", Event{Modality: "image", InputUnits: 1, GeneratedUnits: 1},
			relaycommon.RelayInfo{BillingImageCount: &two}, true, 0, 0, 1},
		{"a rerank's metered search units", Event{Modality: "rerank", GeneratedUnits: 3},
			relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeRerank}, true, 0, 0, 3},
		{"images with the provider's real tokens", Event{Modality: "image", InputUnits: 120, OutputUnits: 4000},
			relaycommon.RelayInfo{BillingImageCount: &two}, false, 120, 4000, 2},
		{"a transcription's input minutes, rounded up", Event{Modality: "audio", InputUnits: 1500},
			relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioTranscription}, false, 0, 0, 2},
		{"speech's output minutes", Event{Modality: "audio", OutputUnits: 1000},
			relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech}, false, 0, 0, 1},
		{"a Realtime session keeps the text tokens the provider billed", Event{Modality: "audio", InputUnits: 351, OutputUnits: 32},
			relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeRealtime}, false, 351, 32, 0},
		{"text is left alone", Event{Modality: "text", InputUnits: 10, OutputUnits: 5},
			relaycommon.RelayInfo{}, false, 10, 5, 0},
	}
	for _, tc := range cases {
		event := tc.event
		measureGenerated(&event, &tc.info, tc.metered)
		assert.Equal(t, [3]int64{tc.input, tc.output, tc.generated}, [3]int64{event.InputUnits, event.OutputUnits, event.GeneratedUnits}, tc.name)
	}
}
