package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	jose "github.com/go-jose/go-jose/v4"
)

// refetchInterval bounds how often the issuer's keys are fetched again. Without it, every token
// naming an unknown key, or failing its signature, would make the Gateway ask Cognito anew, and
// anyone could have Cognito throttle the Gateway for real customers.
const refetchInterval = time.Minute

// cachedKeySet verifies RS256 signatures with the issuer's keys, fetched at most once a
// refetchInterval: a token whose key is still unknown in between is refused at once.
type cachedKeySet struct {
	address string
	mu      sync.Mutex
	keys    map[string]jose.JSONWebKey
	fetched time.Time
}

func newCachedKeySet(address string) *cachedKeySet {
	return &cachedKeySet{address: address, keys: map[string]jose.JSONWebKey{}}
}

// VerifySignature implements oidc.KeySet.
func (k *cachedKeySet) VerifySignature(ctx context.Context, raw string) ([]byte, error) {
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, err
	}
	if len(signed.Signatures) != 1 {
		return nil, errors.New("one signature is expected")
	}
	key, err := k.key(ctx, signed.Signatures[0].Header.KeyID)
	if err != nil {
		return nil, err
	}
	return signed.Verify(key)
}

func (k *cachedKeySet) key(ctx context.Context, id string) (jose.JSONWebKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if key, ok := k.keys[id]; ok {
		return key, nil
	}
	if time.Since(k.fetched) < refetchInterval {
		return jose.JSONWebKey{}, fmt.Errorf("unknown signing key %q", id)
	}
	k.fetched = time.Now()
	keys, err := fetchKeys(ctx, k.address)
	if err != nil {
		return jose.JSONWebKey{}, err
	}
	k.keys = keys
	if key, ok := k.keys[id]; ok {
		return key, nil
	}
	return jose.JSONWebKey{}, fmt.Errorf("unknown signing key %q", id)
}

func fetchKeys(ctx context.Context, address string) (map[string]jose.JSONWebKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching the signing keys: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching the signing keys: %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var set jose.JSONWebKeySet
	if err := common.Unmarshal(body, &set); err != nil {
		return nil, err
	}
	keys := make(map[string]jose.JSONWebKey, len(set.Keys))
	for _, key := range set.Keys {
		keys[key.KeyID] = key
	}
	return keys, nil
}
