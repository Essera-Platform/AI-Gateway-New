package servicetrust

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/awsx"
	"github.com/QuantumNous/new-api/essera/settings"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// reuseFor is how long one signed identity is sent: well within its 15 minutes, and long enough
// that the receiver's own cache spares it most calls to STS.
const reuseFor = 5 * time.Minute

var (
	signedMu    sync.Mutex
	signedCache = map[string]struct {
		header string
		at     time.Time
	}{}
)

// Identity is this Gateway's proof of its task role for the service named by audience: an
// sts:GetCallerIdentity request signed by the role, base64 JSON for the Header. It is the same
// proof the Console sends the Gateway, the other way round.
func Identity(ctx context.Context, audience string) (string, error) {
	now := time.Now()
	signedMu.Lock()
	cached, ok := signedCache[audience]
	signedMu.Unlock()
	if ok && now.Sub(cached.at) < reuseFor {
		return cached.header, nil
	}
	credentials, region, err := awsx.Credentials(ctx)
	if err != nil {
		return "", err
	}
	if region == "" {
		region = "us-east-1"
	}
	endpoint := "https://sts." + region + ".amazonaws.com/"
	if local := settings.AWSEndpoint(); local != "" {
		endpoint = strings.TrimRight(local, "/") + "/"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	request.Header.Set(AudienceHeader, audience)
	digest := sha256.Sum256([]byte(callerIdentityBody))
	if err := v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(digest[:]), "sts", region, now.UTC()); err != nil {
		return "", err
	}
	signed := SignedRequest{Method: http.MethodPost, URL: endpoint, Body: callerIdentityBody, Headers: map[string]string{}}
	for name := range request.Header {
		signed.Headers[name] = request.Header.Get(name)
	}
	encoded, err := common.Marshal(signed)
	if err != nil {
		return "", err
	}
	header := base64.StdEncoding.EncodeToString(encoded)
	signedMu.Lock()
	signedCache[audience] = struct {
		header string
		at     time.Time
	}{header, now}
	signedMu.Unlock()
	return header, nil
}
