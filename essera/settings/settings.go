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

// BridgeBucket is the configuration bucket the bridge objects live in. Normally learned from
// the first configuration event; this setting is for a Gateway that has received none yet.
func BridgeBucket() string {
	return common.GetEnvOrDefaultString("ESSERA_CONFIG_BUCKET", "")
}

// AWSEndpoint points the AWS clients at a local stand-in (MinIO, ElasticMQ) in development.
func AWSEndpoint() string {
	return common.GetEnvOrDefaultString("ESSERA_AWS_ENDPOINT", "")
}

// Enabled is true when the Gateway should connect to the Console's pipeline.
func Enabled() bool {
	return Managed() && Environment() != ""
}
