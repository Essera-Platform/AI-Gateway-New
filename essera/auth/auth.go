// Package auth authenticates the Console's customers: a Cognito client-credentials access
// token from the user pool the Console publishes, whose client is an unexpired credential of
// an active customer in the active configuration (D04 §5). Tenant identity comes from the
// token and the configuration only, never from the request.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/coreos/go-oidc/v3/oidc"
)

// Caller is the authenticated customer of a request.
type Caller struct {
	// ClientID names the credential in usage and limits: a Cognito client id or an API key's
	// key id; "" for a person in the playground, who uses none.
	ClientID   string
	Credential *snapshot.Credential // nil for a person in the playground
	Customer   *snapshot.Organization
	Partner    *snapshot.Organization // nil for a direct customer
	Person     *snapshot.Person       // the key's or the signed-in person; nil for a Cognito client
	View       *snapshot.View
}

// EndUserID is the end user a person's request is made for: the person's own for role user,
// so it cannot be chosen by the request. ok is false for anyone else.
func (c *Caller) EndUserID() (id string, ok bool) {
	if c.Person == nil || c.Person.Role != snapshot.RoleUser {
		return "", false
	}
	if c.Person.EndUserExternalID == nil {
		return "", true
	}
	return *c.Person.EndUserExternalID, true
}

// Failure is why a token was refused; Code is the error code the client sees.
type Failure struct {
	Code    string
	Message string
}

func (f *Failure) Error() string { return f.Message }

func refuse(message string) *Failure {
	return &Failure{Code: "essera_unauthenticated", Message: message}
}

// LooksLikeJWT tells a bearer JWT from a new-api key.
func LooksLikeJWT(value string) bool {
	return strings.HasPrefix(value, "eyJ") && strings.Count(value, ".") == 2
}

// KeySetFor returns the signing keys of an issuer; replaced in tests.
var KeySetFor = remoteKeySet

var (
	keySetsMu sync.Mutex
	keySets   = map[string]oidc.KeySet{}
	client    = &http.Client{Timeout: 10 * time.Second}
)

// remoteKeySet fetches the issuer's signing keys; ESSERA_JWKS_URL replaces the address for a
// local stand-in of Cognito in development.
func remoteKeySet(issuer string) oidc.KeySet {
	keySetsMu.Lock()
	defer keySetsMu.Unlock()
	if keys, ok := keySets[issuer]; ok {
		return keys
	}
	address := common.GetEnvOrDefaultString("ESSERA_JWKS_URL", issuer+"/.well-known/jwks.json")
	keys := newCachedKeySet(address)
	keySets[issuer] = keys
	return keys
}

// IssuerURL is the issuer of the customer pool's tokens.
func IssuerURL() string {
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", settings.CustomerPoolRegion(), settings.CustomerUserPoolID())
}

type claims struct {
	TokenUse string `json:"token_use"`
	ClientID string `json:"client_id"`
	Scope    string `json:"scope"`
}

// Authenticate verifies a bearer token against the active configuration.
func Authenticate(ctx context.Context, rawToken string, view *snapshot.View, now time.Time) (*Caller, *Failure) {
	if view == nil {
		return nil, &Failure{Code: "essera_unavailable", Message: "The gateway has no configuration yet."}
	}
	if settings.CustomerUserPoolID() == "" || len(settings.CustomerScopes()) == 0 {
		return nil, &Failure{Code: "essera_unavailable", Message: "No customer token issuer is configured."}
	}
	issuerURL := IssuerURL()
	verifier := oidc.NewVerifier(issuerURL, KeySetFor(issuerURL), &oidc.Config{
		SkipClientIDCheck:    true, // Cognito access tokens name the client in client_id, not aud
		SupportedSigningAlgs: []string{oidc.RS256},
		Now:                  func() time.Time { return now },
	})
	verified, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, refuse("The access token is not valid.")
	}
	var raw common.RawMessage
	if err := verified.Claims(&raw); err != nil {
		return nil, refuse("The access token is not valid.")
	}
	var parsed claims
	if err := common.Unmarshal(raw, &parsed); err != nil || parsed.TokenUse != "access" || parsed.ClientID == "" {
		return nil, refuse("An access token is required.")
	}
	if !hasScope(parsed.Scope, settings.CustomerScopes()) {
		return nil, refuse("The access token does not carry the gateway scope.")
	}
	return resolve(parsed.ClientID, view, now)
}

func hasScope(granted string, required []string) bool {
	return slices.ContainsFunc(strings.Fields(granted), func(scope string) bool {
		return slices.Contains(required, scope)
	})
}

// resolve finds the credential's customer; a revoked credential is absent from the
// configuration even while tokens issued before the revocation are still signed validly.
func resolve(clientID string, view *snapshot.View, now time.Time) (*Caller, *Failure) {
	credential := view.Credential(clientID)
	if credential == nil {
		return nil, refuse("This credential is not active.")
	}
	if credential.ExpiresAt != nil && !now.Before(*credential.ExpiresAt) {
		return nil, refuse("This credential has expired.")
	}
	customer := view.Customer(credential.CustomerID)
	if customer == nil {
		return nil, refuse("This credential has no customer.")
	}
	return &Caller{
		ClientID:   clientID,
		Credential: credential,
		Customer:   customer,
		Partner:    view.Partner(customer.PartnerID),
		View:       view,
	}, nil
}
