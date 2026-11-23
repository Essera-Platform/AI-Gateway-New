// Added by Essera, 2026-11.
package reasoning

import "regexp"

var bedrockClaudeID = regexp.MustCompile(`^(?:[a-z-]+\.)?anthropic\.(claude-.+)$`)

// anthropicClaudeName turns a Bedrock model id or inference profile id, such
// as eu.anthropic.claude-opus-5 or anthropic.claude-haiku-4-5-20251001-v1:0,
// into the Anthropic name the capability table matches. Other names are
// returned unchanged.
func anthropicClaudeName(model string) string {
	if match := bedrockClaudeID.FindStringSubmatch(model); match != nil {
		return match[1]
	}
	return model
}
