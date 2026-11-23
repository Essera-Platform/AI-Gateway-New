// Package managed keeps a Gateway that the Console administers from being changed or used
// around it: what the Console publishes is not edited locally, and only the request kinds the
// Console can account for are served.
package managed

import (
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/gin-gonic/gin"
)

// customerPaths are the relay routes a person's API key may POST to: the text, embedding,
// rerank and image edit requests Bedrock serves in the Console's region, each producing one
// measured usage event. Legacy completions, image generation, audio, moderations, realtime,
// Gemini's format, tasks, video and plugin routes are not served: Bedrock offers none of them
// there, or they produce no usage event.
var customerPaths = []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/embeddings", "/v1/rerank", "/v1/images/edits"}

// modelsPath answers a GET with the models; nothing else a customer may GET: a GET on
// /v1/responses opens a WebSocket whose calls are not accounted for.
const modelsPath = "/v1/models"

// CustomerRequestAllowed tells whether a customer may make the request.
func CustomerRequestAllowed(method, path string) bool {
	path = strings.TrimSuffix(path, "/")
	switch method {
	case http.MethodGet:
		return path == modelsPath || strings.HasPrefix(path, modelsPath+"/")
	case http.MethodPost:
		return slices.Contains(customerPaths, path)
	}
	return false
}

// RelayPath tells whether the path is served to API keys rather than to the admin UI.
func RelayPath(path string) bool {
	return !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/internal/")
}

// closedPaths are what a managed gateway serves to nobody, whoever asks:
//   - requests the Console cannot account for: Midjourney, Suno, video and tasks, and their logs;
//   - new-api's OAuth sign-in, which could create users the Console does not know, and its
//     password reset and emailed codes: people sign in at /api/essera/sign-in through Cognito;
//   - new-api's commerce, since the Console bills: wallet, top-up, payments and their callbacks,
//     subscriptions, redemption codes, referrals and check-in;
//   - content the Console does not publish: the About page, the site notice and the uptime
//     status; and the public rankings and ratio config, worked out from every customer's
//     traffic and prices;
//   - a new-api key's quota and logs (/api/usage/token, /api/log/token): the Console holds them.
var closedPaths = regexp.MustCompile(strings.Join([]string{
	`^/(mj|suno)(/|$)`, `^/[^/]+/mj/`, `^/v1/(video|videos|tasks)(/|$)`, `^/api/(mj|task)(/|$)`,
	`^/api/oauth/`, `^/api/(verification|reset_password|user/reset)/?$`,
	`^/api/user/(topup|amount|pay|stripe|creem|waffo|waffo-pancake|aff|aff_transfer|epay|checkin)(/|$)`,
	`^/api/(stripe|creem|waffo|waffo-pancake)/webhook(/|$)`, `^/api/(subscription|redemption)(/|$)`,
	`^/api/(about|notice|uptime/status|rankings|ratio_config)/?$`,
	`^/api/usage/token(/|$)`, `^/api/log/token/?$`,
}, "|"))

// personClosed are a signed-in person's account settings that Cognito or the Console own, or
// that serve nothing on a managed gateway: new-api's dashboard access tokens, Passkeys,
// two-factor authentication and the verification they back, sign-in bindings, and the
// notification, unpriced-model and IP-recording settings. Operators (root) keep them.
var personClosed = regexp.MustCompile(`^/api/user/(access_tokens|passkey|2fa|oauth/bindings)(/|$)|^/api/user/setting/?$|^/api/verify(/|$)`)

// PersonRouteRefused refuses a signed-in user below root a route closed to people, and tells
// whether it did. Dashboard authentication calls it once it knows the user's role.
func PersonRouteRefused(c *gin.Context, role int) bool {
	if !settings.Managed() || role >= common.RoleRootUser || !personClosed.MatchString(c.Request.URL.Path) {
		return false
	}
	refuseNotAvailable(c)
	return true
}

// keyWrite tells a person's own key change: create, delete, delete several. The token
// handlers accept these from people only (essera/keys); editing a key and reading a key back
// stay refused, since a key is shown only once, when it is created.
func keyWrite(method, path string) bool {
	switch method {
	case http.MethodPost:
		return path == "/api/token" || path == "/api/token/" || path == "/api/token/batch" || path == "/api/token/batch/"
	case http.MethodDelete:
		return tokenPath.MatchString(path)
	}
	return false
}

var tokenPath = regexp.MustCompile(`^/api/token/\d+/?$`)

// adminWrites change what the Console owns: providers (channels), customers (users),
// credentials (tokens), prices and models; and plugins, deployments and sign-in providers,
// which could run code on requests or reach provider keys around the Console. Reads stay open.
var adminWrites = regexp.MustCompile(`^/api/(channel|token|option|models|vendors|prefill_group|ratio_sync|deployments|plugin|request_plugin|custom-oauth-provider)(/|$)|^/api/user/?$|^/api/user/(manage|register|\d+)(/|$)`)

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
			refuseNotAvailable(c)
			return
		}
		write := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions
		// A person's users row is the Console's projection of them: closing the account is the Console's.
		accountClosing := c.Request.Method == http.MethodDelete && strings.TrimSuffix(path, "/") == "/api/user/self"
		if accountClosing || write && adminWrites.MatchString(path) && !keptWrites.MatchString(path) && !keyWrite(c.Request.Method, path) {
			refuse(c, http.StatusConflict, "managed_by_console", "This is managed in the Essera Operations Console.")
			return
		}
		c.Next()
	}
}

func refuseNotAvailable(c *gin.Context) {
	refuse(c, http.StatusNotFound, "essera_not_available", "This endpoint is not available on the Essera gateway.")
}

func refuse(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message, "error": gin.H{"code": code, "message": message}})
}

// statusSwitchedOff are the /api/status switches of what a managed gateway does not offer:
// announcements, uptime, the API info and FAQ panels, check-in, registration and new-api's OAuth sign-in.
var statusSwitchedOff = []string{
	"announcements_enabled", "uptime_kuma_enabled", "api_info_enabled", "faq_enabled", "checkin_enabled", "register_enabled", "password_register_enabled",
	"github_oauth", "discord_oauth", "linuxdo_oauth", "telegram_oauth", "wechat_login", "oidc_enabled",
}

// TrimStatus takes out of /api/status, on a managed gateway, the chat links and the
// documentation link, and switches off what statusSwitchedOff names, so the web UI offers none
// of them.
func TrimStatus(data map[string]any) {
	if !settings.Managed() {
		return
	}
	data["chats"] = []map[string]string{}
	data["docs_link"] = ""
	for _, key := range statusSwitchedOff {
		data[key] = false
	}
	delete(data, "announcements")
	delete(data, "api_info")
	delete(data, "faq")
	delete(data, "custom_oauth_providers")
}

// AddCatalog gives a managed gateway's status the live count of the models a guest sees in the
// Model Square and of their families, which the public landing page shows. It names no account.
func AddCatalog(data map[string]any) {
	if !settings.Managed() {
		return
	}
	if view := snapshot.Current(); view != nil {
		data["essera_catalog"] = view.Catalog()
	}
}
