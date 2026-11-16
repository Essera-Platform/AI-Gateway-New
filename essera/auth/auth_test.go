package auth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func signer(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	previous := auth.KeySetFor
	auth.KeySetFor = func(string) oidc.KeySet {
		return &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}
	}
	t.Cleanup(func() { auth.KeySetFor = previous })
	return key
}

func token(t *testing.T, key *rsa.PrivateKey, edit func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":       "https://cognito-idp.us-east-1.amazonaws.com/" + snapshottest.UserPoolID,
		"token_use": "access",
		"client_id": snapshottest.ClientID,
		"scope":     snapshottest.Scope,
		"exp":       now.Add(time.Hour).Unix(),
		"iat":       now.Add(-time.Minute).Unix(),
	}
	if edit != nil {
		edit(claims)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestAValidTokenNamesTheCustomerAndPartner(t *testing.T) {
	key := signer(t)

	caller, failure := auth.Authenticate(context.Background(), token(t, key, nil), snapshottest.View(), now)

	require.Nil(t, failure)
	assert.Equal(t, snapshottest.ClientID, caller.ClientID)
	assert.Equal(t, snapshottest.CustomerID, caller.Customer.ID)
	require.NotNil(t, caller.Partner)
	assert.Equal(t, snapshottest.PartnerID, caller.Partner.ID)
}

func TestRefusedTokens(t *testing.T) {
	key := signer(t)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	expiredCredential := snapshottest.Sample()
	ended := now.Add(-time.Second)
	expiredCredential.Accounts.Credentials[0].ExpiresAt = &ended

	cases := []struct {
		name  string
		token string
		view  *snapshot.View
	}{
		{"expired token", token(t, key, func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() }), snapshottest.View()},
		{"signed by someone else", token(t, other, nil), snapshottest.View()},
		{"another pool", token(t, key, func(c jwt.MapClaims) { c["iss"] = "https://cognito-idp.us-east-1.amazonaws.com/other" }), snapshottest.View()},
		{"an id token", token(t, key, func(c jwt.MapClaims) { c["token_use"] = "id" }), snapshottest.View()},
		{"without the gateway scope", token(t, key, func(c jwt.MapClaims) { c["scope"] = "profile" }), snapshottest.View()},
		{"a revoked credential", token(t, key, func(c jwt.MapClaims) { c["client_id"] = "revoked-client" }), snapshottest.View()},
		{"a credential past its expiry", token(t, key, nil), snapshot.NewView(8, expiredCredential)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caller, failure := auth.Authenticate(context.Background(), tc.token, tc.view, now)
			assert.Nil(t, caller)
			require.NotNil(t, failure)
			assert.Equal(t, "essera_unauthenticated", failure.Code)
		})
	}
}

func TestNoConfigurationMeansUnavailable(t *testing.T) {
	_, failure := auth.Authenticate(context.Background(), "eyJ.a.b", nil, now)
	require.NotNil(t, failure)
	assert.Equal(t, "essera_unavailable", failure.Code)
}

func TestLooksLikeJWT(t *testing.T) {
	assert.True(t, auth.LooksLikeJWT("eyJhbGciOi.eyJzdWIi.c2ln"))
	assert.False(t, auth.LooksLikeJWT("sk-abc-123"))
	assert.False(t, auth.LooksLikeJWT("eyJonly.two"))
}
