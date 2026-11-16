// Essera fork: connects new-api's channel test to the Console's connection tests (M02-C07).
package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/essera/bootstrap"
	"github.com/QuantumNous/new-api/essera/bridge"
	"github.com/QuantumNous/new-api/model"
)

func init() {
	bridge.RunChannelTest = runEsseraChannelTest
}

// runEsseraChannelTest tests a provider's channel as the Essera service user: one minimal
// request with the configured key, reporting success, latency and the provider's error.
func runEsseraChannelTest(ctx context.Context, channel *model.Channel) bridge.TestResult {
	var user model.User
	if err := model.DB.Select("id").Where("username = ?", bootstrap.ServiceUsername).First(&user).Error; err != nil {
		return bridge.TestResult{Message: "The Gateway's service user is missing."}
	}
	started := time.Now()
	result := testChannel(ctx, channel, user.Id, "", "", false)
	latency := time.Since(started)
	switch {
	case result.localErr != nil:
		return bridge.TestResult{Message: result.localErr.Error()}
	case result.newAPIError != nil:
		return bridge.TestResult{Latency: latency, Message: result.newAPIError.Error()}
	}
	go channel.UpdateResponseTime(latency.Milliseconds())
	return bridge.TestResult{Succeeded: true, Latency: latency}
}
