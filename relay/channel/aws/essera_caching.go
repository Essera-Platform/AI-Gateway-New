package aws

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/settings"
)

// promptCachingOff is true on a managed Gateway: the Console does not yet bill cache reads and
// writes as their own units, so no request may ask Bedrock to cache.
var promptCachingOff = settings.Managed

var cachingFields = []string{"cache_control", "prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"}

// withoutPromptCaching drops every prompt-caching marker from an Anthropic Messages body: the
// request's own, and those on system blocks, tools, message content and tool results.
func withoutPromptCaching(body io.Reader) (io.Reader, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	var request map[string]json.RawMessage
	if err := common.Unmarshal(payload, &request); err != nil {
		return bytes.NewReader(payload), nil
	}
	for _, field := range cachingFields {
		delete(request, field)
	}
	for _, field := range []string{"system", "tools"} {
		if value, ok := request[field]; ok {
			request[field] = withoutBlockCaching(value)
		}
	}
	if value, ok := request["messages"]; ok {
		var messages []map[string]json.RawMessage
		if common.Unmarshal(value, &messages) == nil {
			for _, message := range messages {
				if content, ok := message["content"]; ok {
					message["content"] = withoutBlockCaching(content)
				}
			}
			if encoded, err := common.Marshal(messages); err == nil {
				request["messages"] = encoded
			}
		}
	}
	encoded, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(encoded), nil
}

// withoutBlockCaching drops cache_control from each block of a list, and from the blocks a tool
// result holds; anything that is not a list of blocks is kept as it is.
func withoutBlockCaching(value json.RawMessage) json.RawMessage {
	var blocks []map[string]json.RawMessage
	if common.Unmarshal(value, &blocks) != nil {
		return value
	}
	for _, block := range blocks {
		delete(block, "cache_control")
		if content, ok := block["content"]; ok {
			block["content"] = withoutBlockCaching(content)
		}
	}
	encoded, err := common.Marshal(blocks)
	if err != nil {
		return value
	}
	return encoded
}
