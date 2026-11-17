// Package managed keeps a Gateway that the Console administers from being changed or used
// around it: what the Console publishes is not edited locally, and only the request kinds the
// Console can account for are served.
package managed

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/gin-gonic/gin"
)

// customerPaths are the synchronous relay routes a customer's credential may call: each
// produces one measured usage event. Task, video and plugin routes do not.
var customerPaths = []string{
	"/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/messages", "/v1/embeddings",
	"/v1/images/generations", "/v1/images/edits", "/v1/audio/", "/v1/rerank", "/v1/moderations",
	"/v1/models", "/v1beta/models", "/v1beta/openai/",
}

// listingPaths answer a GET with the models; nothing else a customer may GET: a GET on
// /v1/responses opens a WebSocket whose calls are not accounted for.
var listingPaths = []string{"/v1/models", "/v1beta/models"}

// longRunning marks Gemini's asynchronous operations, which run as tasks without a usage event.
const longRunning = ":predictLongRunning"

// CustomerRequestAllowed tells whether a customer may make the request.
func CustomerRequestAllowed(method, path string) bool {
	if method == http.MethodGet {
		return matches(listingPaths, path)
	}
	return matches(customerPaths, path) && !strings.HasSuffix(path, longRunning)
}

// RelayPath tells whether the path is served to API keys rather than to the admin UI.
func RelayPath(path string) bool {
	return !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/internal/")
}

func matches(paths []string, path string) bool {
	for _, allowed := range paths {
		if path == allowed || strings.HasPrefix(path, allowed+"/") || (strings.HasSuffix(allowed, "/") && strings.HasPrefix(path, allowed)) {
			return true
		}
	}
	return false
}

// closedPaths serve requests the Console cannot account for: the playground, asynchronous
// tasks, video and Midjourney.
var closedPaths = regexp.MustCompile(`^/(pg|mj|suno)(/|$)|^/[^/]+/mj/|^/v1/(video|videos|tasks)(/|$)`)

// adminWrites change what the Console owns: providers (channels), customers (users),
// credentials (tokens), prices, models and commerce; and plugins and sign-in providers, which
// could run code on requests or reach provider keys around the Console. Reads stay open.
var adminWrites = regexp.MustCompile(`^/api/(channel|token|option|models|vendors|prefill_group|subscription|ratio_sync|redemption|deployments|plugin|request_plugin|custom-oauth-provider)(/|$)|^/api/user/?$|^/api/user/(manage|topup/complete|register|\d+)(/|$)`)

// keptWrites stay the Gateway's own even when managed: routing and health policies.
var keptWrites = regexp.MustCompile(`^/api/option/request_policy`)

// Guard refuses closed routes and local edits of what the Console owns.
func Guard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settings.Managed() {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if closedPaths.MatchString(path) {
			refuse(c, http.StatusNotFound, "essera_not_available", "This endpoint is not available on the Essera gateway.")
			return
		}
		write := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions
		if write && adminWrites.MatchString(path) && !keptWrites.MatchString(path) {
			refuse(c, http.StatusConflict, "managed_by_console", "This is managed in the Essera Operations Console.")
			return
		}
		c.Next()
	}
}

func refuse(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message, "error": gin.H{"code": code, "message": message}})
}
