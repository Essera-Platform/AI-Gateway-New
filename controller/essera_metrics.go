// Essera's model metrics on a managed gateway (CHANGES-ESSERA.md): the Console measures each
// model's availability and speed and is their only source; the Gateway counts none of its own
// here. When the Console does not answer, nothing is shown rather than old or zero figures.
package controller

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/essera/consoleapi"
	"github.com/gin-gonic/gin"
)

// esseraModelMetrics reads the Console's metrics of the person's models, by provider/model;
// nil when the Console does not answer.
func esseraModelMetrics(ctx context.Context, personID string) map[string]*consoleapi.ModelMetric {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	metrics, err := consoleapi.GetModelMetrics(ctx, personID)
	if err != nil {
		return nil
	}
	byModel := map[string]*consoleapi.ModelMetric{}
	for i := range metrics {
		byModel[metrics[i].Provider+"/"+metrics[i].Model] = &metrics[i]
	}
	return byModel
}

const (
	platformMetricsFresh = time.Minute
	// A Console that did not answer is asked again only after this, so the public landing page
	// never has every visitor wait on it.
	platformMetricsRetry = 15 * time.Second
)

var platformMetrics struct {
	mu      sync.Mutex
	value   *consoleapi.PlatformMetrics
	readAt  time.Time
	keepFor time.Duration
}

// currentPlatformMetrics is the Console's platform summary, read at most once a minute; nil
// while the Console does not answer or has no figures.
func currentPlatformMetrics(ctx context.Context, now time.Time) *consoleapi.PlatformMetrics {
	platformMetrics.mu.Lock()
	defer platformMetrics.mu.Unlock()
	if !platformMetrics.readAt.IsZero() && now.Sub(platformMetrics.readAt) < platformMetrics.keepFor {
		return platformMetrics.value
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	read, err := consoleapi.GetPlatformMetrics(ctx)
	platformMetrics.readAt = now
	if err != nil || read.Availability == nil {
		platformMetrics.value, platformMetrics.keepFor = nil, platformMetricsRetry
		return nil
	}
	platformMetrics.value, platformMetrics.keepFor = read, platformMetricsFresh
	return read
}

// EsseraPlatformMetrics serves the landing page the platform's availability and median time to
// first token over the last 7 days: platform-wide, so open to guests. data is null while the
// Console does not answer.
func EsseraPlatformMetrics(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": currentPlatformMetrics(c.Request.Context(), time.Now())})
}
