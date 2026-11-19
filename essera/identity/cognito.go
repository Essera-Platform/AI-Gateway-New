package identity

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/settings"
)

// CognitoEndpoint is the user-pool API of a region; replaced in tests.
var CognitoEndpoint = func(region string) string {
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/", region)
}

var cognitoClient = &http.Client{Timeout: 10 * time.Second}

// cognitoError is an error the user-pool API answered, by its exception name.
type cognitoError struct {
	Type    string
	Message string
	Status  int
}

func (e *cognitoError) Error() string { return "cognito " + e.Type + ": " + e.Message }

// refused tells an answer about the credentials or the user (wrong password or code, unknown or
// disabled user, a spent session or refresh token) from a fault of the service or the client.
func (e *cognitoError) refused() bool {
	switch e.Type {
	case "NotAuthorizedException", "UserNotFoundException", "CodeMismatchException", "ExpiredCodeException",
		"UserNotConfirmedException", "PasswordResetRequiredException", "TooManyFailedAttemptsException":
		return true
	}
	return false
}

type authenticationResult struct {
	IdToken      string `json:"IdToken"`
	RefreshToken string `json:"RefreshToken"`
	ExpiresIn    int    `json:"ExpiresIn"`
}

type challengeAnswer struct {
	ChallengeName        string                `json:"ChallengeName"`
	Session              string                `json:"Session"`
	ChallengeParameters  map[string]string     `json:"ChallengeParameters"`
	AvailableChallenges  []string              `json:"AvailableChallenges"`
	AuthenticationResult *authenticationResult `json:"AuthenticationResult"`
}

// poolClient is the app client the Gateway signs people in with, in the pool the issuer names.
type poolClient struct {
	settings.SignInClient
	region string
}

func signInClient() (poolClient, bool) {
	client, ok := settings.OIDCClient()
	if !ok {
		return poolClient{}, false
	}
	host, found := strings.CutPrefix(client.Issuer, "https://cognito-idp.")
	region, _, _ := strings.Cut(host, ".")
	return poolClient{SignInClient: client, region: region}, found && region != ""
}

// secretHash proves the client secret on calls that name a user, as the pool requires of an
// app client that has one.
func (p poolClient) secretHash(username string) string {
	mac := hmac.New(sha256.New, []byte(p.ClientSecret))
	mac.Write([]byte(username + p.ClientID))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (p poolClient) call(ctx context.Context, operation string, input, output any) error {
	body, err := common.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, CognitoEndpoint(p.region), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", "AWSCognitoIdentityProviderService."+operation)
	response, err := cognitoClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
		}
		_ = common.Unmarshal(data, &failure)
		_, name, found := strings.Cut(failure.Type, "#")
		if !found {
			name = failure.Type
		}
		return &cognitoError{Type: name, Message: failure.Message, Status: response.StatusCode}
	}
	if output == nil {
		return nil
	}
	return common.Unmarshal(data, output)
}
