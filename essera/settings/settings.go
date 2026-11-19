// Package settings reads how this Gateway is connected to the Essera Operations Console.
package settings

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Managed is true when the Console administers this Gateway: providers, customers and
// limits come from its published configuration, and local edits of them are refused.
func Managed() bool {
	return common.GetEnvOrDefaultBool("ESSERA_MANAGED", false)
}

// Environment is dev, uat or prod: the prefix of every object and queue the Gateway uses.
func Environment() string {
	return strings.ToLower(strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_ENVIRONMENT", "")))
}

func ConfigQueueName() string {
	return common.GetEnvOrDefaultString("ESSERA_CONFIG_QUEUE", fmt.Sprintf("essera-%s-config-updates", Environment()))
}

func UsageQueueName() string {
	return common.GetEnvOrDefaultString("ESSERA_USAGE_QUEUE", fmt.Sprintf("essera-%s-usage-events", Environment()))
}

// CustomerUserPoolID is the Cognito user pool the Console issues customer credentials in.
// The Console's own variable name is accepted too, so one definition can feed both services.
func CustomerUserPoolID() string {
	pool := common.GetEnvOrDefaultString("ESSERA_COGNITO_USER_POOL_ID", common.GetEnvOrDefaultString("CUSTOMER_USER_POOL_ID", ""))
	return strings.TrimSpace(pool)
}

// CustomerPoolRegion is the pool's region, by default the one its id begins with.
func CustomerPoolRegion() string {
	region, _, _ := strings.Cut(CustomerUserPoolID(), "_")
	return common.GetEnvOrDefaultString("ESSERA_COGNITO_REGION", region)
}

// CustomerScopes are the scopes the Console grants customer credentials (the Console's
// CUSTOMER_OAUTH_SCOPES); a token must carry one of them.
func CustomerScopes() []string {
	var scopes []string
	configured := common.GetEnvOrDefaultString("ESSERA_CUSTOMER_SCOPES", common.GetEnvOrDefaultString("CUSTOMER_OAUTH_SCOPES", ""))
	for scope := range strings.SplitSeq(configured, ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// AWSEndpoint points the AWS clients at a local stand-in (MinIO, ElasticMQ) in development.
func AWSEndpoint() string {
	return common.GetEnvOrDefaultString("ESSERA_AWS_ENDPOINT", "")
}

// Enabled is true when the Gateway should connect to the Console's pipeline.
func Enabled() bool {
	return Managed() && Environment() != ""
}

// Group is the routing group of every projected channel, of the service user and of every
// person: a managed gateway offers no other.
const Group = "essera"

// SignInClient is the Cognito app client people sign in to the Gateway's web UI with, through
// the user-pool API: the Gateway's own sign-in page, never Cognito's hosted one.
type SignInClient struct {
	Issuer       string // https://cognito-idp.<region>.amazonaws.com/<pool id>
	ClientID     string
	ClientSecret string
}

// OIDCClient reads the sign-in client from the environment; ok is false unless all of it is set.
func OIDCClient() (client SignInClient, ok bool) {
	client = SignInClient{
		Issuer:       strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_OIDC_ISSUER", "")), "/"),
		ClientID:     strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_OIDC_CLIENT_ID", "")),
		ClientSecret: strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_OIDC_CLIENT_SECRET", "")),
	}
	return client, client.Issuer != "" && client.ClientID != "" && client.ClientSecret != ""
}

const (
	SignInPassword = "password"
	SignInEmailOTP = "email_otp"
)

// SignInMode is how people sign in: email and a one-time code (the default; the pool must send
// email through SES), or email and password when ESSERA_SIGN_IN=password.
func SignInMode() string {
	if strings.EqualFold(strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_SIGN_IN", "")), SignInPassword) {
		return SignInPassword
	}
	return SignInEmailOTP
}

// SystemName is the gateway's name in its web UI and emails; "Essera AI Gateway" unless
// ESSERA_SYSTEM_NAME names it otherwise.
func SystemName() string {
	return strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_SYSTEM_NAME", "Essera AI Gateway"))
}

// Logo is the address of the logo the web UI shows, from ESSERA_LOGO; empty keeps new-api's.
func Logo() string {
	return strings.TrimSpace(common.GetEnvOrDefaultString("ESSERA_LOGO", ""))
}
