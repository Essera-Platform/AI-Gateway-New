// Package secrets resolves provider keys the Console names by Secrets Manager reference, so a
// key is never written to the Gateway's database (D04: the Gateway retrieves provider
// credentials only through its runtime identity).
package secrets

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
)

// Prefix marks a channel key that is a reference: "secretsmanager:<name or ARN>", optionally
// followed by "|<suffix>" that is appended to the resolved value (Bedrock's "|<region>").
const Prefix = "secretsmanager:"

const ttl = 5 * time.Minute

type cached struct {
	value   string
	fetched time.Time
}

// fetch reads a secret; replaced in tests.
var fetch = awsx.SecretString

var (
	mu    sync.Mutex
	cache = map[string]cached{}
)

// Reference builds the channel key for a secret reference.
func Reference(secret string, suffix string) string {
	if suffix == "" {
		return Prefix + secret
	}
	return Prefix + secret + "|" + suffix
}

// Resolve returns the key a channel key stands for. handled is false for an ordinary key.
func Resolve(channelKey string) (value string, handled bool, err error) {
	rest, ok := strings.CutPrefix(channelKey, Prefix)
	if !ok {
		return "", false, nil
	}
	reference, suffix, _ := strings.Cut(rest, "|")
	value, err = lookup(reference)
	if err != nil {
		return "", true, err
	}
	if suffix != "" && !strings.Contains(value, "|") {
		value += "|" + suffix
	}
	return value, true, nil
}

// Forget drops every cached value, so a new configuration version reads secrets afresh.
func Forget() {
	mu.Lock()
	defer mu.Unlock()
	clear(cache)
}

func lookup(reference string) (string, error) {
	mu.Lock()
	entry, ok := cache[reference]
	mu.Unlock()
	if ok && time.Since(entry.fetched) < ttl {
		return entry.value, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := fetch(ctx, reference)
	if err != nil {
		return "", err
	}
	value := keyFromSecret(raw)
	mu.Lock()
	cache[reference] = cached{value: value, fetched: time.Now()}
	mu.Unlock()
	return value, nil
}

// keyFromSecret accepts a plain key or a JSON object holding it under a usual name.
func keyFromSecret(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	var fields map[string]any
	if err := common.UnmarshalJsonStr(trimmed, &fields); err != nil {
		return trimmed
	}
	for _, name := range []string{"api_key", "apiKey", "key", "value", "secret"} {
		if value, ok := fields[name].(string); ok && value != "" {
			return value
		}
	}
	// A service-account document (Vertex) is the key itself.
	return trimmed
}
