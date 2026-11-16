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
