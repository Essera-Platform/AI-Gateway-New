package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	calls := 0
	values := map[string]string{
		"plain":   "sk-provider",
		"json":    `{"api_key": "from-json"}`,
		"bedrock": "bedrock-api-key",
		"vertex":  `{"type": "service_account", "project_id": "p"}`,
	}
	previous := fetch
	fetch = func(_ context.Context, reference string) (string, error) {
		calls++
		if value, ok := values[reference]; ok {
			return value, nil
		}
		return "", errors.New("access denied")
	}
	t.Cleanup(func() { fetch = previous; Forget() })
	Forget()

	cases := []struct{ channelKey, want string }{
		{Reference("plain", ""), "sk-provider"},
		{Reference("json", ""), "from-json"},
		{Reference("bedrock", "us-east-1"), "bedrock-api-key|us-east-1"},
		{Reference("vertex", ""), `{"type": "service_account", "project_id": "p"}`},
	}
	for _, tc := range cases {
		value, handled, err := Resolve(tc.channelKey)
		require.NoError(t, err)
		assert.True(t, handled)
		assert.Equal(t, tc.want, value)
	}
	_, _, _ = Resolve(Reference("plain", ""))
	assert.Equal(t, 4, calls, "a value is cached")

	_, handled, err := Resolve("sk-ordinary-key")
	assert.False(t, handled)
	assert.NoError(t, err)

	_, handled, err = Resolve(Reference("missing", ""))
	assert.True(t, handled)
	assert.Error(t, err)
}
