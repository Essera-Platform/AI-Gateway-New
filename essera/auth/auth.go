// Package auth holds who a customer request is made by, and the signing keys of the customer
// pool. A relay request is authenticated only by an API key a person created and the Console
// published (essera/keys), or as the signed-in person in the playground (essera/identity);
// tenant identity comes from the key or the person and the configuration only, never from the
// request. The Cognito client-credentials tokens accepted before are refused (CHANGES-ESSERA.md).
package auth

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/coreos/go-oidc/v3/oidc"
)

// Caller is the authenticated customer of a request.
type Caller struct {
	// CredentialRef names the credential in usage and limits: the API key's key id; "" for a
	// person in the playground, who uses none.
	CredentialRef string
	Credential    *snapshot.Credential // nil for a person in the playground
	Customer      *snapshot.Organization
	Partner       *snapshot.Organization // nil for a direct customer
	Person        *snapshot.Person       // the key's or the signed-in person
	View          *snapshot.View
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

// Failure is why a request was refused; Code is the error code the client sees.
type Failure struct {
	Code    string
	Message string
}

func (f *Failure) Error() string { return f.Message }

// KeySetFor returns the signing keys of an issuer, the customer pool whose ID tokens sign
// people in; replaced in tests.
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
