// Essera fork: the request field that carries reasoning_effort per model family.
package aws

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReasoningEffortTakesTheFieldEachFamilyAccepts(t *testing.T) {
	for _, model := range []string{"openai.gpt-5.5", "us.openai.gpt-5.6-sol", "openai.gpt-oss-120b-1:0", "us.moonshotai.kimi-k3"} {
		assert.Equal(t, map[string]any{"reasoning": map[string]any{"effort": "low"}}, converseReasoningFields(model, "minimal"), model)
	}
	assert.Nil(t, converseReasoningFields("us.xai.grok-4.7", "high"), "Grok takes any field and shows no effect of one")
	assert.Nil(t, converseReasoningFields("deepseek.v3.2", "high"))
}
