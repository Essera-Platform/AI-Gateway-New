package servicetrust

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
	} {
		request := signed(host, goodAuthorization, Audience())
		assert.Equal(t, allowed, checkShape(request) == nil, host)
	}
}
