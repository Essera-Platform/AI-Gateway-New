package aws

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/usage"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const cachingContextKey = "essera_prompt_caching"

// cachingFor is the prompt caching a request may ask Bedrock for. On a managed Gateway the
// Console bills cache reads and writes as their own units, so a request may cache only for a
// model whose cache units have cost and selling rates (policy.PromptCaching); a request
// without an Essera caller, such as a connection test, caches nothing.
var cachingFor = func(c *gin.Context, info *relaycommon.RelayInfo) policy.Caching {
	if !settings.Managed() {
		return policy.CachingAll
	}
	value, ok := c.Get(usage.ContextKey)
	if !ok {
		return policy.CachingOff
	}
	request, _ := value.(*policy.Request)
	return request.PromptCaching(usage.ProviderOf(info.ChannelId), info.OriginModelName, time.Now())
}

// decideCaching keeps the request's caching for the conversions that do not see the relay info.
func decideCaching(c *gin.Context, info *relaycommon.RelayInfo) policy.Caching {
	caching := cachingFor(c, info)
	c.Set(cachingContextKey, caching)
	return caching
}

// cachingDecided is the caching decided for the request, all of it outside a managed Gateway.
func cachingDecided(c *gin.Context) policy.Caching {
	if value, ok := c.Get(cachingContextKey); ok {
		if caching, ok := value.(policy.Caching); ok {
			return caching
		}
	}
	if settings.Managed() {
		return policy.CachingOff
	}
	return policy.CachingAll
}

// withCaching fits an Anthropic Messages body to the caching allowed: unchanged for all of it,
// the 1-hour TTL dropped (writes then use the 5-minute default) when only that is priced, and
// every caching marker dropped when none is.
func withCaching(body io.Reader, caching policy.Caching) (io.Reader, error) {
	switch caching {
	case policy.CachingAll:
		return body, nil
	case policy.CachingFiveMinutes:
		return rewriteCacheControl(body, withoutLongTTL)
	}
	return withoutPromptCaching(body)
}

var promptCacheFields = []string{"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"}

// cacheControlFix turns one cache_control value into the one to send; false drops it.
type cacheControlFix func(json.RawMessage) (json.RawMessage, bool)

func dropCacheControl(json.RawMessage) (json.RawMessage, bool) { return nil, false }

// withoutLongTTL drops a TTL other than the 5-minute default, so the write is a 5-minute one.
func withoutLongTTL(value json.RawMessage) (json.RawMessage, bool) {
	var control map[string]json.RawMessage
	if common.Unmarshal(value, &control) != nil {
		return value, true
	}
	var ttl string
	if raw, ok := control["ttl"]; !ok || (common.Unmarshal(raw, &ttl) == nil && ttl == "5m") {
		return value, true
	}
	delete(control, "ttl")
	encoded, err := common.Marshal(control)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// withoutPromptCaching drops every prompt-caching marker from an Anthropic Messages body: the
// request's own, and those on system blocks, tools, message content and tool results.
func withoutPromptCaching(body io.Reader) (io.Reader, error) {
	return rewriteCacheControl(body, dropCacheControl)
}

// rewriteCacheControl applies fix to each cache_control of an Anthropic Messages body: the
// request's own, and those on system blocks, tools, message content and tool results. Dropping
// them all drops the OpenAI prompt_cache_* fields too.
func rewriteCacheControl(body io.Reader, fix cacheControlFix) (io.Reader, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	var request map[string]json.RawMessage
	if err := common.Unmarshal(payload, &request); err != nil {
		return bytes.NewReader(payload), nil
	}
	applyFix(request, fix)
	if _, keep := fix(json.RawMessage(`{}`)); !keep {
		for _, field := range promptCacheFields {
			delete(request, field)
		}
	}
	for _, field := range []string{"system", "tools"} {
		if value, ok := request[field]; ok {
			request[field] = blocksFixed(value, fix)
		}
	}
	if value, ok := request["messages"]; ok {
		var messages []map[string]json.RawMessage
		if common.Unmarshal(value, &messages) == nil {
			for _, message := range messages {
				if content, ok := message["content"]; ok {
					message["content"] = blocksFixed(content, fix)
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

func applyFix(object map[string]json.RawMessage, fix cacheControlFix) {
	value, ok := object["cache_control"]
	if !ok {
		return
	}
	if fixed, keep := fix(value); keep {
		object["cache_control"] = fixed
	} else {
		delete(object, "cache_control")
	}
}

// blocksFixed applies fix to the cache_control of each block of a list, and of the blocks a
// tool result holds; anything that is not a list of blocks is kept as it is.
func blocksFixed(value json.RawMessage, fix cacheControlFix) json.RawMessage {
	var blocks []map[string]json.RawMessage
	if common.Unmarshal(value, &blocks) != nil {
		return value
	}
	for _, block := range blocks {
		applyFix(block, fix)
		if content, ok := block["content"]; ok {
			block["content"] = blocksFixed(content, fix)
		}
	}
	encoded, err := common.Marshal(blocks)
	if err != nil {
		return value
	}
	return encoded
}
