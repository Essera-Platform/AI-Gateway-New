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

var stsHost = regexp.MustCompile(`^sts(\.[a-z0-9-]+)?\.amazonaws\.com(\.cn)?$`)

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

var (
	cacheMu sync.Mutex
	cache   = map[string]verified{}
	client  = &http.Client{Timeout: 5 * time.Second}
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
	if err := checkShape(request); err != nil {
		return "", err
	}
	authorization := header(request.Headers, "Authorization")
	cacheMu.Lock()
	hit, ok := cache[authorization]
	cacheMu.Unlock()
	if ok && time.Since(hit.at) < 5*time.Minute {
		return hit.arn, nil
	}
	arn, err := callerIdentity(ctx, request)
	if err != nil {
		return "", err
	}
	if !trusted(arn) {
		return "", fmt.Errorf("%s is not trusted", arn)
	}
	cacheMu.Lock()
	cache[authorization] = verified{arn: arn, at: time.Now()}
	cacheMu.Unlock()
	return arn, nil
}

func header(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func checkShape(request SignedRequest) error {
	target, err := url.Parse(request.URL)
	if err != nil || request.Method != http.MethodPost || !stsEndpointAllowed(target) {
		return errors.New("the identity is not an STS request")
	}
	if request.Body != callerIdentityBody {
		return errors.New("the identity is not GetCallerIdentity")
	}
	if header(request.Headers, AudienceHeader) != Audience() {
		return errors.New("the identity is signed for another service")
	}
	authorization := header(request.Headers, "Authorization")
	_, signed, found := strings.Cut(authorization, "SignedHeaders=")
	signed, _, _ = strings.Cut(signed, ",")
	if !found || !strings.Contains(";"+strings.ToLower(signed)+";", ";"+strings.ToLower(AudienceHeader)+";") {
		return errors.New("the audience is not signed")
	}
	return nil
}

var arnPattern = regexp.MustCompile(`<Arn>([^<]+)</Arn>`)

func callerIdentity(ctx context.Context, request SignedRequest) (string, error) {
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodPost, request.URL, bytes.NewBufferString(request.Body))
	if err != nil {
		return "", err
	}
	for key, value := range request.Headers {
		outgoing.Header.Set(key, value)
	}
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
