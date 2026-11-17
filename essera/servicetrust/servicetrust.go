// Package servicetrust authenticates the Platform Backend on the Gateway's internal API by its
// AWS identity (ADR-008: service trust by IAM over a private network, no shared secret).
//
// The Backend signs an sts:GetCallerIdentity request with its task role and sends the signed
// request in a header. The Gateway forwards it to STS: only the holder of the role's
// credentials can produce a signature STS accepts, and STS answers with the role's ARN, which
// must be one this Gateway trusts. The signature covers an audience header naming this
// Gateway, so a request signed for one service cannot be replayed to another.
package servicetrust

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/settings"
)

// Header carries the signed request, base64 of SignedRequest as JSON.
const Header = "X-Essera-Iam"

// AudienceHeader is the signed header naming the service the signature is for.
const AudienceHeader = "X-Essera-Audience"

const callerIdentityBody = "Action=GetCallerIdentity&Version=2011-06-15"

// SignedRequest is an sts:GetCallerIdentity request as the caller signed it.
type SignedRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
}

// Audience is the value the caller must sign: this Gateway in this environment.
func Audience() string {
	return "essera-gateway-" + settings.Environment()
}

// trustedPrincipals are the ARN patterns allowed to call, "*" matching any run of characters
// within a path segment (arn:aws:sts::123456789012:assumed-role/essera-dev-task/*).
func trustedPrincipals() []string {
	var patterns []string
	for pattern := range strings.SplitSeq(common.GetEnvOrDefaultString("ESSERA_TRUSTED_PRINCIPALS", ""), ",") {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

// stsHost is STS's global or regional endpoint and nothing else under amazonaws.com: a looser
// pattern would let sts.<anything>.amazonaws.com, such as an S3 website, answer for STS.
var stsHost = regexp.MustCompile(`^sts(\.[a-z]{2}(-gov|-iso[a-z]?)?-[a-z]+-[0-9])?\.amazonaws\.com(\.cn)?$`)

// stsEndpointAllowed accepts only AWS STS, or the local stand-in in development, so the
// Gateway never forwards a request elsewhere.
func stsEndpointAllowed(target *url.URL) bool {
	if local := settings.AWSEndpoint(); local != "" {
		if base, err := url.Parse(local); err == nil && base.Host == target.Host {
			return true
		}
	}
	return target.Scheme == "https" && stsHost.MatchString(target.Hostname())
}

type verified struct {
	arn string
	at  time.Time
}

// signatureLifetime is how long AWS accepts a SigV4 signature after its X-Amz-Date.
const signatureLifetime = 15 * time.Minute

var (
	cacheMu sync.Mutex
	cache   = map[[32]byte]verified{}
	client  = &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
)

// Verify returns the caller's ARN when the signed request proves a trusted identity.
func Verify(ctx context.Context, encoded string) (string, error) {
	if encoded == "" {
		return "", errors.New("no identity")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("unreadable identity")
	}
	var request SignedRequest
	if err := common.Unmarshal(raw, &request); err != nil {
		return "", errors.New("unreadable identity")
	}
	headers, err := canonical(request.Headers)
	if err != nil {
		return "", err
	}
	if err := checkShape(request, headers); err != nil {
		return "", err
	}
	expires, err := signatureExpiry(headers)
	if err != nil {
		return "", err
	}
	now := time.Now()
	key := sha256.Sum256(raw)
	cacheMu.Lock()
	hit, ok := cache[key]
	cacheMu.Unlock()
	if ok && now.Sub(hit.at) < 5*time.Minute && now.Before(expires) {
		return hit.arn, nil
	}
	arn, err := callerIdentity(ctx, request, headers)
	if err != nil {
		return "", err
	}
	if !trusted(arn) {
		return "", fmt.Errorf("%s is not trusted", arn)
	}
	cacheMu.Lock()
	for stale, entry := range cache {
		if now.Sub(entry.at) >= 5*time.Minute {
			delete(cache, stale)
		}
	}
	cache[key] = verified{arn: arn, at: now}
	cacheMu.Unlock()
	return arn, nil
}

// canonical is the one set of headers that is both checked and forwarded; a name given twice
// in different cases is refused, so the check and STS cannot see different values.
func canonical(given map[string]string) (http.Header, error) {
	headers := http.Header{}
	for name, value := range given {
		key := http.CanonicalHeaderKey(name)
		if _, twice := headers[key]; twice {
			return nil, fmt.Errorf("the identity repeats the header %s", key)
		}
		headers.Set(key, value)
	}
	return headers, nil
}

// signatureExpiry is when AWS stops accepting the signature; a verified identity is not reused
// past it.
func signatureExpiry(headers http.Header) (time.Time, error) {
	signedAt, err := time.Parse("20060102T150405Z", headers.Get("X-Amz-Date"))
	if err != nil {
		return time.Time{}, errors.New("the identity has no signing time")
	}
	return signedAt.Add(signatureLifetime), nil
}

func checkShape(request SignedRequest, headers http.Header) error {
	target, err := url.Parse(request.URL)
	if err != nil || request.Method != http.MethodPost || !stsEndpointAllowed(target) {
		return errors.New("the identity is not an STS request")
	}
	if request.Body != callerIdentityBody {
		return errors.New("the identity is not GetCallerIdentity")
	}
	if headers.Get(AudienceHeader) != Audience() {
		return errors.New("the identity is signed for another service")
	}
	authorization := headers.Get("Authorization")
	_, signed, found := strings.Cut(authorization, "SignedHeaders=")
	signed, _, _ = strings.Cut(signed, ",")
	if !found || !strings.Contains(";"+strings.ToLower(signed)+";", ";"+strings.ToLower(AudienceHeader)+";") {
		return errors.New("the audience is not signed")
	}
	return nil
}

var arnPattern = regexp.MustCompile(`<Arn>([^<]+)</Arn>`)

func callerIdentity(ctx context.Context, request SignedRequest, headers http.Header) (string, error) {
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodPost, request.URL, bytes.NewBufferString(request.Body))
	if err != nil {
		return "", err
	}
	outgoing.Header = headers.Clone()
	response, err := client.Do(outgoing)
	if err != nil {
		return "", fmt.Errorf("STS: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("STS refused the identity (%d)", response.StatusCode)
	}
	match := arnPattern.FindSubmatch(body)
	if match == nil {
		return "", errors.New("STS returned no identity")
	}
	return string(match[1]), nil
}

func trusted(arn string) bool {
	for _, pattern := range trustedPrincipals() {
		if ok, _ := path.Match(pattern, arn); ok {
			return true
		}
	}
	return false
}
