package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sign(t *testing.T, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", kid))
	require.NoError(t, err)
	signed, err := signer.Sign([]byte(`{"sub":"x"}`))
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	return compact
}

func TestAnUnknownKeyIsNotFetchedAgainWithinAMinute(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var fetches atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		body, _ := common.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
		_, _ = w.Write(body)
	}))
	t.Cleanup(issuer.Close)
	keys := newCachedKeySet(issuer.URL)

	payload, err := keys.VerifySignature(context.Background(), sign(t, key, "k1"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"sub":"x"}`, string(payload))

	for range 20 {
		_, err = keys.VerifySignature(context.Background(), sign(t, key, "unknown"))
		assert.Error(t, err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	_, err = keys.VerifySignature(context.Background(), sign(t, other, "k1"))
	assert.Error(t, err, "a forged signature under a known key")

	assert.Equal(t, int32(1), fetches.Load(), "unknown keys and forged signatures do not reach the issuer")
}
