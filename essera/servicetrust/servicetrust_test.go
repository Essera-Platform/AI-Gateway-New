package servicetrust

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeSTS(t *testing.T, arn string) *httptest.Server {
	t.Helper()
	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "bad" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`<GetCallerIdentityResponse><GetCallerIdentityResult><Arn>` + arn + `</Arn></GetCallerIdentityResult></GetCallerIdentityResponse>`))
	}))
	t.Cleanup(sts.Close)
	t.Setenv("ESSERA_AWS_ENDPOINT", sts.URL)
	t.Setenv("ESSERA_ENVIRONMENT", "dev")
	t.Setenv("ESSERA_TRUSTED_PRINCIPALS", "arn:aws:sts::123456789012:assumed-role/essera-dev-task/*")
	return sts
}

func encode(t *testing.T, request SignedRequest) string {
	t.Helper()
	raw, err := common.Marshal(request)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func signed(url, authorization, audience string) SignedRequest {
	return SignedRequest{
		Method: http.MethodPost, URL: url, Body: callerIdentityBody,
		Headers: map[string]string{
			"Authorization":     authorization,
			"X-Essera-Audience": audience,
			"X-Amz-Date":        time.Now().UTC().Format("20060102T150405Z"),
		},
	}
}

const goodAuthorization = "AWS4-HMAC-SHA256 Credential=AKIA/20261116/us-east-1/sts/aws4_request, SignedHeaders=content-type;host;x-amz-date;x-essera-audience, Signature=abc"

func TestATrustedRoleIsAccepted(t *testing.T) {
	sts := fakeSTS(t, "arn:aws:sts::123456789012:assumed-role/essera-dev-task/abc123")

	arn, err := Verify(context.Background(), encode(t, signed(sts.URL, goodAuthorization, "essera-gateway-dev")))

	require.NoError(t, err)
	assert.Equal(t, "arn:aws:sts::123456789012:assumed-role/essera-dev-task/abc123", arn)
}

func TestRefusedIdentities(t *testing.T) {
	sts := fakeSTS(t, "arn:aws:sts::123456789012:assumed-role/other-role/abc")
	unsignedAudience := "AWS4-HMAC-SHA256 Credential=AKIA/x, SignedHeaders=content-type;host;x-amz-date, Signature=abc"
	cases := map[string]SignedRequest{
		"an untrusted role":              signed(sts.URL, goodAuthorization+"1", "essera-gateway-dev"),
		"signed for another service":     signed(sts.URL, goodAuthorization+"2", "essera-gateway-prod"),
		"audience outside the signature": signed(sts.URL, unsignedAudience, "essera-gateway-dev"),
		"not sent to STS":                signed("https://example.com/", goodAuthorization+"3", "essera-gateway-dev"),
		"STS refuses the signature":      signed(sts.URL, "bad", "essera-gateway-dev"),
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Verify(context.Background(), encode(t, request))
			assert.Error(t, err)
		})
	}
	other := signed(sts.URL, goodAuthorization+"4", "essera-gateway-dev")
	other.Body = "Action=AssumeRole"
	_, err := Verify(context.Background(), encode(t, other))
	assert.Error(t, err, "only GetCallerIdentity")
	_, err = Verify(context.Background(), "")
	assert.Error(t, err)
}

func TestSTSHosts(t *testing.T) {
	t.Setenv("ESSERA_AWS_ENDPOINT", "")
	for host, allowed := range map[string]bool{
		"https://sts.amazonaws.com/":                        true,
		"https://sts.us-east-1.amazonaws.com/":              true,
		"http://sts.us-east-1.amazonaws.com/":               false,
		"https://sts.us-east-1.amazonaws.com.evil.example/": false,
		"https://evil.example/":                             false,
		"https://sts.s3-website-us-east-1.amazonaws.com/":   false,
		"https://sts.us-gov-west-1.amazonaws.com/":          true,
	} {
		request := signed(host, goodAuthorization, Audience())
		headers, err := canonical(request.Headers)
		require.NoError(t, err)
		assert.Equal(t, allowed, checkShape(request, headers) == nil, host)
	}
}

func TestAHeaderGivenTwiceInDifferentCasesIsRefused(t *testing.T) {
	sts := fakeSTS(t, "arn:aws:sts::123456789012:assumed-role/essera-dev-task/abc123")
	request := signed(sts.URL, goodAuthorization+"5", "essera-gateway-dev")
	request.Headers["x-essera-audience"] = "essera-gateway-prod"

	_, err := Verify(context.Background(), encode(t, request))

	assert.ErrorContains(t, err, "repeats the header")
}

func TestAnIdentityPastItsSignatureLifetimeIsNotReused(t *testing.T) {
	sts := fakeSTS(t, "arn:aws:sts::123456789012:assumed-role/essera-dev-task/abc123")
	request := signed(sts.URL, goodAuthorization+"6", "essera-gateway-dev")
	request.Headers["X-Amz-Date"] = time.Now().UTC().Add(-20 * time.Minute).Format("20060102T150405Z")
	encoded := encode(t, request)
	_, err := Verify(context.Background(), encoded)
	require.NoError(t, err, "the fake STS accepts it; real STS refuses a signature this old")

	cacheMu.Lock()
	_, cached := cache[sha256.Sum256(mustDecode(t, encoded))]
	cacheMu.Unlock()
	require.True(t, cached)
	sts.Close() // a reuse would now have to come from the cache
	_, err = Verify(context.Background(), encoded)
	assert.Error(t, err, "past X-Amz-Date + 15 minutes the cache does not answer")
}

func mustDecode(t *testing.T, encoded string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	return raw
}

// TestTheGatewaysOwnIdentityIsAValidSignatureForItsAudience signs as the Console's read API
// expects and checks the signature the way STS does, by signing the same request again.
func TestTheGatewaysOwnIdentityIsAValidSignatureForItsAudience(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("AWS_REGION", "eu-west-1")
	credentials := aws.Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", SessionToken: "session-token"}
	var checked int
	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.Equal(t, callerIdentityBody, string(body))
		signedAt, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
		require.NoError(t, err)
		_, list, _ := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
		list, _, _ = strings.Cut(list, ",")
		again, _ := http.NewRequest(http.MethodPost, "http://"+r.Host+"/", nil)
		for name := range strings.SplitSeq(list, ";") {
			if name != "host" {
				again.Header.Set(name, r.Header.Get(name))
			}
		}
		again.Header.Del("X-Amz-Security-Token")
		digest := sha256.Sum256(body)
		require.NoError(t, v4.NewSigner().SignHTTP(context.Background(), credentials, again, hex.EncodeToString(digest[:]), "sts", "eu-west-1", signedAt))
		if again.Header.Get("Authorization") != r.Header.Get("Authorization") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		checked++
		_, _ = w.Write([]byte(`<GetCallerIdentityResponse><GetCallerIdentityResult><Arn>arn:aws:sts::123456789012:assumed-role/essera-dev-task/gw</Arn></GetCallerIdentityResult></GetCallerIdentityResponse>`))
	}))
	t.Cleanup(sts.Close)
	t.Setenv("ESSERA_AWS_ENDPOINT", sts.URL)
	t.Setenv("ESSERA_ENVIRONMENT", "dev")
	t.Setenv("ESSERA_TRUSTED_PRINCIPALS", "arn:aws:sts::123456789012:assumed-role/essera-dev-task/*")

	header, err := Identity(context.Background(), Audience())
	require.NoError(t, err)
	var request SignedRequest
	require.NoError(t, common.Unmarshal(mustDecode(t, header), &request))
	assert.Equal(t, Audience(), request.Headers[AudienceHeader])
	assert.Contains(t, request.Headers["Authorization"], "x-essera-audience", "the audience is signed")
	assert.Equal(t, "session-token", request.Headers["X-Amz-Security-Token"])

	arn, err := Verify(context.Background(), header)
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:sts::123456789012:assumed-role/essera-dev-task/gw", arn)
	assert.Equal(t, 1, checked)

	again, err := Identity(context.Background(), Audience())
	require.NoError(t, err)
	assert.Equal(t, header, again, "one signature is sent for a while, so the receiver's cache answers")
	other, err := Identity(context.Background(), "essera-console-dev")
	require.NoError(t, err)
	assert.NotEqual(t, header, other, "each audience gets its own")
}
