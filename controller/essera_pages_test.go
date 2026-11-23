// Essera's tests of the pages a managed gateway serves its people from the Console
// (CHANGES-ESSERA.md), against a stand-in of the Console's internal API.
package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/apidocs"
	"github.com/QuantumNous/new-api/essera/availability"
	"github.com/QuantumNous/new-api/essera/consoleapi"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type consoleCall struct {
	method, path, query, person, identity, body string
}

// fakeConsole answers the Console's internal API with canned bodies by "METHOD /path".
type fakeConsole struct {
	mu      sync.Mutex
	calls   []consoleCall
	answers map[string]func(w http.ResponseWriter)
	server  *httptest.Server
}

func (f *fakeConsole) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.calls = append(f.calls, consoleCall{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get(consoleapi.PersonHeader), r.Header.Get("X-Essera-Iam"), string(body)})
	answer := f.answers[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	if answer == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	answer(w)
}

func (f *fakeConsole) on(route string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[route] = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func (f *fakeConsole) lastCall(t *testing.T) consoleCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.calls)
	return f.calls[len(f.calls)-1]
}

func (f *fakeConsole) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// consolePages applies the sample configuration (its people become users) and stands up the
// Console. It returns the Console and the user id of each person by Cognito sub.
func consolePages(t *testing.T) (*fakeConsole, map[string]int) {
	t.Helper()
	esseraSignInSetup(t, "email_otp")
	console := &fakeConsole{answers: map[string]func(http.ResponseWriter){}}
	console.server = httptest.NewServer(console)
	t.Cleanup(console.server.Close)
	t.Setenv("CONSOLE_INTERNAL_URL", console.server.URL)
	t.Setenv("ESSERA_ENVIRONMENT", "dev")
	previous := consoleapi.Identity
	consoleapi.Identity = func(_ context.Context, audience string) (string, error) { return "signed-for-" + audience, nil }
	t.Cleanup(func() { consoleapi.Identity = previous })
	var users []model.User
	require.NoError(t, model.DB.Where("oidc_id <> ''").Find(&users).Error)
	bySub := map[string]int{}
	for _, user := range users {
		bySub[user.OidcId] = user.Id
	}
	return console, bySub
}

type pageResponse struct {
	status  int
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
	List    []any          `json:"-"`
	raw     string
}

func page(t *testing.T, userID int, method, path, body string, handler gin.HandlerFunc, route string) pageResponse {
	t.Helper()
	engine := gin.New()
	engine.Handle(method, route, func(c *gin.Context) {
		c.Set("id", userID)
		var user model.User
		if model.DB.First(&user, userID).Error == nil {
			c.Set("role", user.Role)
			c.Set("username", user.Username)
		}
	}, handler)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	response := pageResponse{status: recorder.Code, raw: recorder.Body.String()}
	var envelope struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope), recorder.Body.String())
	response.Success, response.Message = envelope.Success, envelope.Message
	switch data := envelope.Data.(type) {
	case map[string]any:
		response.Data = data
	case []any:
		response.List = data
	}
	return response
}

func TestAPersonsUsageLogsAreTheConsolesLedgerInNewAPIsFormat(t *testing.T) {
	console, users := consolePages(t)
	console.on("GET /internal/v1/usage", http.StatusOK, `{"data":[{"ts":"2026-11-19T10:00:00Z","request_id":"req-1","provider":"bedrock",
		"model":"claude-sonnet","modality":"text","input_units":1200,"output_units":340,"generated_units":0,"billable_amount":"0.009150",
		"cache_read_units":5000,"cache_write_units":800,"cache_write_1h_units":200,"input_image_units":0,
		"units":{"input_tokens":1200,"cache_read_tokens":5000,"cache_write_tokens":800,"cache_write_1h_tokens":200,"output_tokens":340},
		"charges":[{"unit":"input_tokens","quantity":1200,"amount":"0.003600","currency":"USD"},{"unit":"cache_read_tokens","quantity":5000,"amount":"0.001500","currency":"USD"},
		{"unit":"cache_write_tokens","quantity":800,"amount":"0.003000","currency":"USD"},{"unit":"cache_write_1h_tokens","quantity":200,"amount":null,"currency":null}],
		"billable_currency":"USD","status":"succeeded","key_id":"k_unknown","end_user":"user-1","latency_ms":2400}],"total":41,"page":2,"page_size":20}`)

	response := page(t, users[snapshottest.AdminSub], http.MethodGet, "/api/log/self?p=2&page_size=20&model_name=claude-sonnet&start_timestamp=1795000000", "", GetUserLogs, "/api/log/self")

	require.True(t, response.Success, response.raw)
	call := console.lastCall(t)
	assert.Equal(t, snapshottest.AdminPersonID, call.person, "the Console scopes the answer to the signed-in person")
	assert.Equal(t, "signed-for-essera-console-dev", call.identity, "signed for the Console of this environment")
	assert.Contains(t, call.query, "model=claude-sonnet")
	assert.Contains(t, call.query, "page=2")
	assert.Contains(t, call.query, "from=2026-11-18T")
	assert.EqualValues(t, 41, response.Data["total"])
	items := response.Data["items"].([]any)
	require.Len(t, items, 1)
	log := items[0].(map[string]any)
	assert.Equal(t, "claude-sonnet", log["model_name"])
	assert.EqualValues(t, 1200, log["prompt_tokens"])
	assert.EqualValues(t, 340, log["completion_tokens"])
	assert.EqualValues(t, 2, log["use_time"])
	assert.Equal(t, "user-1@acme.example", log["username"], "the end user by their person's email")
	assert.Empty(t, log["group"], "a managed gateway shows no groups")
	assert.EqualValues(t, int(0.00915*common.QuotaPerUnit+0.5), log["quota"])
	assert.Contains(t, log["other"], `"billable_amount":"0.009150"`)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log["other"].(string), &other))
	assert.EqualValues(t, 5000, other["cache_tokens"], "cache reads where new-api's log views read them")
	assert.EqualValues(t, 800, other["cache_creation_tokens_5m"])
	assert.EqualValues(t, 200, other["cache_creation_tokens_1h"])
	assert.EqualValues(t, 1000, other["cache_creation_tokens"])
	assert.EqualValues(t, 5000, other["essera"].(map[string]any)["units"].(map[string]any)["cache_read_tokens"], "every unit measured, for the details")
	charges := other["essera"].(map[string]any)["charges"].([]any)
	require.Len(t, charges, 4, "what each unit came to, as the Console rated it")
	assert.Equal(t, map[string]any{"unit": "cache_read_tokens", "quantity": float64(5000), "amount": "0.001500", "currency": "USD"}, charges[1])
	assert.Nil(t, charges[3].(map[string]any)["amount"], "an unpriced unit stays unpriced")

	console.on("GET /internal/v1/usage", http.StatusBadGateway, `{}`)
	down := page(t, users[snapshottest.AdminSub], http.MethodGet, "/api/log/self", "", GetUserLogs, "/api/log/self")
	assert.False(t, down.Success)
	assert.Equal(t, "Data is temporarily unavailable", down.Message)
}

func TestTheAccountSummaryIsTheConsolesOrUnavailable(t *testing.T) {
	console, users := consolePages(t)
	console.on("GET /internal/v1/me", http.StatusOK, `{"person":{"id":"`+snapshottest.UserPersonID+`","email":"user-1@acme.example","role":"user","status":"active","end_user_external_id":"user-1"},
		"customer":{"id":"`+snapshottest.CustomerID+`","name":"Acme","status":"active"},
		"month":{"since":"2026-11-01T00:00:00Z","requests":42,"input_units":1000,"output_units":500,"generated_units":0,"cache_read_units":300,"cache_write_units":20,"cache_write_1h_units":10,
		"units":{"input_tokens":1000,"cache_read_tokens":300,"cache_write_tokens":20,"cache_write_1h_tokens":10,"output_tokens":500,"images":4},"spend":{"USD":"3.840000"}},
		"limits":{"monthly_request_quota":1000,"monthly_token_quota":null,"monthly_spend_limit":"50.00","currency":"USD"},
		"remaining":{"requests":958,"tokens":null,"spend":"46.160000"}}`)

	response := page(t, users[snapshottest.UserSub], http.MethodGet, "/api/user/self", "", GetSelf, "/api/user/self")

	require.True(t, response.Success, response.raw)
	account := response.Data["essera"].(map[string]any)
	assert.Equal(t, true, account["available"])
	assert.Equal(t, "3.840000", account["spend"])
	assert.Equal(t, "50.00", account["spend_limit"])
	assert.Equal(t, "46.160000", account["spend_left"])
	assert.EqualValues(t, 1830, account["tokens"], "cached tokens count with the rest, images do not")
	assert.EqualValues(t, 4, account["units"].(map[string]any)["images"])
	assert.Equal(t, "Acme", account["customer_name"])

	console.server.Close()
	down := page(t, users[snapshottest.UserSub], http.MethodGet, "/api/user/self", "", GetSelf, "/api/user/self")
	require.True(t, down.Success, "the profile still loads when the Console is down")
	assert.Equal(t, false, down.Data["essera"].(map[string]any)["available"])
}

func TestOperatorsKeepNewAPIsOwnPages(t *testing.T) {
	console, _ := consolePages(t)
	root := model.User{Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "root", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&root).Error)

	response := page(t, root.Id, http.MethodGet, "/api/user/self", "", GetSelf, "/api/user/self")

	require.True(t, response.Success, response.raw)
	assert.NotContains(t, response.Data, "essera")
	assert.Zero(t, console.callCount(), "an operator's pages never ask the Console")
}

func TestOnlyAnAdminSeesAndAddsTheCustomersPeople(t *testing.T) {
	console, users := consolePages(t)
	console.on("GET /internal/v1/people", http.StatusOK, `{"data":[{"id":"`+snapshottest.AdminPersonID+`","email":"owner@acme.example","name":null,"role":"admin","status":"active","end_user_external_id":null,"keys":1},
		{"id":"`+snapshottest.UserPersonID+`","email":"user-1@acme.example","name":"User One","role":"user","status":"active","end_user_external_id":"user-1","keys":2}],"total":2,"page":1,"page_size":20}`)
	console.on("POST /internal/v1/people", http.StatusCreated, `{"id":"new-person","email":"new@acme.example","name":"New One","role":"user","status":"active","end_user_external_id":"new-1","keys":0}`)

	for name, userID := range map[string]int{"an end user": users[snapshottest.UserSub], "a suspended person": users[snapshottest.SuspendedSub]} {
		listed := page(t, userID, http.MethodGet, "/api/essera/people", "", EsseraPeople, "/api/essera/people")
		assert.Equal(t, http.StatusForbidden, listed.status, name)
		added := page(t, userID, http.MethodPost, "/api/essera/people", `{"email":"new@acme.example"}`, EsseraAddPerson, "/api/essera/people")
		assert.Equal(t, http.StatusForbidden, added.status, name)
	}
	assert.Zero(t, console.callCount(), "refused on the Gateway before the Console is asked")

	listed := page(t, users[snapshottest.AdminSub], http.MethodGet, "/api/essera/people?p=1&page_size=20", "", EsseraPeople, "/api/essera/people")
	require.True(t, listed.Success, listed.raw)
	items := listed.Data["items"].([]any)
	require.Len(t, items, 2)
	assert.Equal(t, true, items[0].(map[string]any)["self"])
	assert.EqualValues(t, 2, items[1].(map[string]any)["key_count"])
	assert.Equal(t, "User One", items[1].(map[string]any)["name"])
	assert.Nil(t, items[0].(map[string]any)["name"], "the admin has no name")

	added := page(t, users[snapshottest.AdminSub], http.MethodPost, "/api/essera/people", `{"email":" New@Acme.example ","name":"New One"}`, EsseraAddPerson, "/api/essera/people")
	require.Equal(t, http.StatusCreated, added.status, added.raw)
	assert.Equal(t, true, added.Data["pending"], "shown until the Console publishes them")
	call := console.lastCall(t)
	assert.Equal(t, snapshottest.AdminPersonID, call.person)
	assert.JSONEq(t, `{"email":"new@acme.example","name":"New One"}`, call.body)
	assert.Equal(t, "New One", added.Data["name"])

	invalid := page(t, users[snapshottest.AdminSub], http.MethodPost, "/api/essera/people", `{"email":"not-an-email"}`, EsseraAddPerson, "/api/essera/people")
	assert.Equal(t, http.StatusBadRequest, invalid.status)

	console.on("POST /internal/v1/people", http.StatusConflict, `{"error":{"message":"duplicate"}}`)
	duplicate := page(t, users[snapshottest.AdminSub], http.MethodPost, "/api/essera/people", `{"email":"user-1@acme.example"}`, EsseraAddPerson, "/api/essera/people")
	assert.Equal(t, http.StatusConflict, duplicate.status)
	assert.Equal(t, "A person with this email already exists.", duplicate.Message)

	console.on("POST /internal/v1/people", http.StatusForbidden, `{"error":{"message":"suspended"}}`)
	refused := page(t, users[snapshottest.AdminSub], http.MethodPost, "/api/essera/people", `{"email":"other@acme.example"}`, EsseraAddPerson, "/api/essera/people")
	assert.Equal(t, http.StatusForbidden, refused.status, "the Console's word: a suspended tenant")

	console.server.Close()
	down := page(t, users[snapshottest.AdminSub], http.MethodPost, "/api/essera/people", `{"email":"other@acme.example"}`, EsseraAddPerson, "/api/essera/people")
	assert.False(t, down.Success)
	assert.Equal(t, "Data is temporarily unavailable", down.Message)
}

func TestOnlyAnAdminSeesTheServiceStatusFromTheConsole(t *testing.T) {
	console, users := consolePages(t)
	console.on("GET /internal/v1/status", http.StatusOK, `{"providers":[{"name":"AWS Bedrock","region":"eu-west-1","status":"degraded","availability_last_hour":93.5,"p50_ms":840,"checked_at":"2026-11-20T09:30:00Z","models":12}],"maintenance":{"status":"scheduled","message":"Upgrade","starts_at":"2026-11-22T02:00:00Z","ends_at":"2026-11-22T03:00:00Z"}}`)
	get := func(userID int) pageResponse {
		return page(t, userID, http.MethodGet, "/api/essera/status", "", EsseraStatus, "/api/essera/status")
	}

	for name, userID := range map[string]int{"an end user": users[snapshottest.UserSub], "a suspended person": users[snapshottest.SuspendedSub]} {
		assert.Equal(t, http.StatusForbidden, get(userID).status, name)
	}
	assert.Zero(t, console.callCount(), "refused on the Gateway before the Console is asked")

	shown := get(users[snapshottest.AdminSub])
	require.True(t, shown.Success, shown.raw)
	assert.Equal(t, snapshottest.AdminPersonID, console.lastCall(t).person)
	assert.Equal(t, true, shown.Data["available"])
	providers := shown.Data["providers"].([]any)
	require.Len(t, providers, 1)
	assert.Equal(t, map[string]any{"name": "AWS Bedrock", "region": "eu-west-1", "status": "degraded", "availability_last_hour": 93.5, "p50_ms": float64(840), "checked_at": "2026-11-20T09:30:00Z", "models": float64(12)}, providers[0])
	assert.Equal(t, "scheduled", shown.Data["maintenance"].(map[string]any)["status"])

	console.on("GET /internal/v1/status", http.StatusInternalServerError, `{}`)
	failing := get(users[snapshottest.AdminSub])
	require.True(t, failing.Success, failing.raw)
	assert.Equal(t, map[string]any{"available": false}, failing.Data)

	console.server.Close()
	down := get(users[snapshottest.AdminSub])
	require.True(t, down.Success, down.raw)
	assert.Equal(t, map[string]any{"available": false}, down.Data, "said to be unavailable, not guessed")
}

func TestAnAdminSuspendsReactivatesAndRemovesOnlyTheirOwnUsers(t *testing.T) {
	console, users := consolePages(t)
	admin := users[snapshottest.AdminSub]
	console.on("POST /internal/v1/people/"+snapshottest.UserPersonID+"/suspend", http.StatusOK, `{"id":"`+snapshottest.UserPersonID+`","email":"user-1@acme.example","role":"user","status":"suspended","end_user_external_id":"user-1","keys":2}`)
	console.on("POST /internal/v1/people/"+snapshottest.SuspendedPersonID+"/reactivate", http.StatusOK, `{"id":"`+snapshottest.SuspendedPersonID+`","email":"gone@acme.example","role":"user","status":"active","end_user_external_id":null,"keys":0}`)
	console.on("DELETE /internal/v1/people/"+snapshottest.UserPersonID, http.StatusNoContent, ``)
	change := func(userID int, method, path string) pageResponse {
		route := "/api/essera/people/:id/:action"
		if method == http.MethodDelete {
			route = "/api/essera/people/:id"
		}
		return page(t, userID, method, path, "", EsseraChangePerson, route)
	}

	refusals := map[string]struct {
		userID int
		method string
		path   string
		status int
	}{
		"an end user":                {users[snapshottest.UserSub], http.MethodPost, "/api/essera/people/" + snapshottest.SuspendedPersonID + "/suspend", http.StatusForbidden},
		"another tenant's person":    {admin, http.MethodPost, "/api/essera/people/" + snapshottest.DirectPersonID + "/suspend", http.StatusNotFound},
		"another customer's admin":   {users[snapshottest.DirectSub], http.MethodDelete, "/api/essera/people/" + snapshottest.UserPersonID, http.StatusNotFound},
		"the owner, by themselves":   {admin, http.MethodPost, "/api/essera/people/" + snapshottest.AdminPersonID + "/suspend", http.StatusForbidden},
		"removing their own account": {admin, http.MethodDelete, "/api/essera/people/" + snapshottest.AdminPersonID, http.StatusForbidden},
		"an unknown person":          {admin, http.MethodDelete, "/api/essera/people/no-such-person", http.StatusNotFound},
		"an unknown action":          {admin, http.MethodPost, "/api/essera/people/" + snapshottest.UserPersonID + "/promote", http.StatusNotFound},
	}
	for name, attempt := range refusals {
		response := change(attempt.userID, attempt.method, attempt.path)
		assert.Equal(t, attempt.status, response.status, name)
		assert.False(t, response.Success, name)
	}
	assert.Zero(t, console.callCount(), "every refusal is the Gateway's own, before the Console")

	suspended := change(admin, http.MethodPost, "/api/essera/people/"+snapshottest.UserPersonID+"/suspend")
	require.True(t, suspended.Success, suspended.raw)
	assert.Equal(t, "suspended", suspended.Data["status"])
	assert.Equal(t, true, suspended.Data["pending"])
	assert.Equal(t, snapshottest.AdminPersonID, console.lastCall(t).person)

	reactivated := change(admin, http.MethodPost, "/api/essera/people/"+snapshottest.SuspendedPersonID+"/reactivate")
	require.True(t, reactivated.Success, reactivated.raw)
	assert.Equal(t, "active", reactivated.Data["status"])

	removed := change(admin, http.MethodDelete, "/api/essera/people/"+snapshottest.UserPersonID)
	require.True(t, removed.Success, removed.raw)
	assert.Equal(t, http.MethodDelete, console.lastCall(t).method)

	console.on("POST /internal/v1/people/"+snapshottest.UserPersonID+"/suspend", http.StatusConflict, `{}`)
	conflict := change(admin, http.MethodPost, "/api/essera/people/"+snapshottest.UserPersonID+"/suspend")
	assert.Equal(t, http.StatusConflict, conflict.status)

	console.server.Close()
	down := change(admin, http.MethodPost, "/api/essera/people/"+snapshottest.UserPersonID+"/suspend")
	assert.Equal(t, "Data is temporarily unavailable", down.Message)
}

func TestTheModelSquareShowsAPersonOnlyTheirModelsWithTheConsolesPrices(t *testing.T) {
	console, users := consolePages(t)
	limited := snapshottest.Sample()
	limited.Accounts.EndUsers[0].Limits.Models = []string{"openai/gpt-4o"} // user-1, narrower than the customer
	snapshot.Activate(snapshot.NewView(4, limited))
	console.on("GET /internal/v1/prices", http.StatusOK, `{"data":[
		{"provider":"openai","model":"gpt-4o","unit":"input_tokens","amount":"2.40","per_quantity":1000000,"currency":"USD"},
		{"provider":"openai","model":"gpt-4o","unit":"output_tokens","amount":"12.00","per_quantity":1000000,"currency":"USD"}]}`)
	console.on("GET /internal/v1/model-metrics", http.StatusOK, `{"data":[
		{"provider":"openai","model":"gpt-4o","availability_24h":99.5,"p50_ttft_ms":420,"p95_ttft_ms":900,
		 "p50_latency_ms":1100,"output_per_second":85.2,"traffic_requests_24h":40,"probe_requests_24h":48,
		 "checked_at":"2026-11-20T10:00:00Z"},
		{"provider":"bedrock","model":"gpt-4o","availability_24h":90,"p50_ttft_ms":700,"p95_ttft_ms":null,
		 "p50_latency_ms":null,"output_per_second":null,"traffic_requests_24h":0,"probe_requests_24h":2,
		 "checked_at":"2026-11-20T10:00:00Z"}],"computed_at":"2026-11-20T10:01:00Z"}`)
	pricing := func(userID int) (map[string]any, []string, []any) {
		engine := gin.New()
		engine.GET("/api/pricing", func(c *gin.Context) {
			if userID > 0 {
				c.Set("id", userID)
			}
		}, GetPricing)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/pricing", nil))
		var body map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
		var names []string
		models := body["data"].([]any)
		for _, entry := range models {
			item := entry.(map[string]any)
			names = append(names, item["owner_by"].(string)+"/"+item["model_name"].(string))
		}
		return body["essera"].(map[string]any), names, models
	}

	unavailable := types.WithOpenAIError(types.OpenAIError{Message: "AWS Bedrock: Bedrock is unable to process your request.", Code: "service_unavailable"}, http.StatusServiceUnavailable)
	availability.Record("claude-sonnet", unavailable)
	availability.Record("claude-sonnet", unavailable)
	t.Cleanup(func() { availability.Record("claude-sonnet", nil) })

	guest, names, models := pricing(0)
	assert.Equal(t, false, guest["signed_in"])
	assert.Equal(t, false, guest["metrics_available"], "a guest sees no metrics, not that a model has none")
	for _, entry := range models {
		item := entry.(map[string]any)
		if item["model_name"] == "claude-sonnet" {
			assert.Equal(t, true, item["temporarily_unavailable"], "its recent calls all failed at AWS")
		} else {
			assert.NotContains(t, item, "temporarily_unavailable")
		}
	}
	assert.Zero(t, console.callCount(), "a guest's list never asks the Console")
	assert.ElementsMatch(t, []string{"OpenAI/gpt-4o", "Amazon Bedrock/claude-sonnet", "Amazon Bedrock/gpt-4o", "Acme LLM/acme-1"}, names,
		"a guest: the whole active catalog, without the model disabled in it (gpt-old)")
	for _, entry := range models {
		assert.NotContains(t, entry, "essera_prices", "a guest sees no prices")
		assert.NotContains(t, entry, "essera_metrics", "a guest sees no metrics")
	}
	first := models[0].(map[string]any)
	assert.Equal(t, "GPT-4o", first["display_name"])
	assert.Empty(t, first["enable_groups"], "no groups on a managed gateway")
	assert.Equal(t, "text", first["model_type"])
	assert.Equal(t, []any{"max_tokens", "stream"}, first["supported_parameters"], "a model missing from the parameter table gets the safe set")
	assert.NotContains(t, first, "sampling_exclusive")

	_, names, _ = pricing(users[snapshottest.AdminSub])
	assert.ElementsMatch(t, []string{"OpenAI/gpt-4o", "Amazon Bedrock/claude-sonnet"}, names,
		"an admin: the customer's entitlements only; Bedrock's gpt-4o and Acme are not entitled, gpt-old is disabled")

	signedIn, names, models := pricing(users[snapshottest.UserSub])
	assert.Equal(t, map[string]any{"signed_in": true, "prices_available": true, "metrics_available": true}, signedIn)
	assert.Equal(t, []string{"OpenAI/gpt-4o"}, names, "a user: their own limits within the customer's")
	assert.Equal(t, snapshottest.UserPersonID, console.lastCall(t).person)
	prices := models[0].(map[string]any)["essera_prices"].([]any)
	require.Len(t, prices, 2)
	assert.Equal(t, "2.40", prices[0].(map[string]any)["amount"], "the Console's price as it gives it")
	metrics := models[0].(map[string]any)["essera_metrics"].(map[string]any)
	assert.Equal(t, 99.5, metrics["availability_24h"], "OpenAI's gpt-4o, not Bedrock's model of the same name")
	assert.EqualValues(t, 420, metrics["p50_ttft_ms"])
	assert.Equal(t, 85.2, metrics["output_per_second"])
	assert.Equal(t, "2026-11-20T10:00:00Z", metrics["checked_at"])

	console.on("GET /internal/v1/model-metrics", http.StatusOK, `{"data":[]}`)
	none, _, models := pricing(users[snapshottest.UserSub])
	assert.Equal(t, true, none["metrics_available"], "the Console answered: a model without metrics has had no requests")
	assert.NotContains(t, models[0], "essera_metrics")

	_, names, _ = pricing(users[snapshottest.SuspendedSub])
	assert.Empty(t, names, "a suspended person sees nothing")

	console.server.Close()
	down, names, models := pricing(users[snapshottest.UserSub])
	assert.Equal(t, map[string]any{"signed_in": true, "prices_available": false, "metrics_available": false}, down)
	assert.Equal(t, []string{"OpenAI/gpt-4o"}, names, "the list stays when the Console is down")
	assert.NotContains(t, models[0], "essera_metrics", "no old or zero figures while the Console is down")
}

func TestTheDashboardAndAuditOfAPersonAreTheConsoles(t *testing.T) {
	console, users := consolePages(t)
	console.on("GET /internal/v1/usage/series", http.StatusOK, `{"bucket":"hour","rows":[
		{"ts":"2026-11-19T10:00:00Z","provider":"bedrock","model":"claude-sonnet","requests":2,"input_units":300,"output_units":100,"generated_units":0,"cache_read_units":50,"cache_write_units":25,"cache_write_1h_units":5,
		"units":{"input_tokens":300,"cache_read_tokens":50,"cache_write_tokens":25,"cache_write_1h_tokens":5,"output_tokens":100,"speech_input_tokens":20},"spend":{"USD":"0.030000"}},
		{"ts":"2026-11-19T11:00:00Z","provider":"openai","model":"gpt-4o","requests":1,"input_units":10,"output_units":5,"generated_units":0,"spend":{"EUR":"0.004000","USD":"0.001000"}},
		{"ts":"2026-11-19T11:00:00Z","provider":"bedrock","model":"cohere.rerank-v3-5:0","requests":1,"input_units":0,"output_units":0,"generated_units":3,"units":{"search_units":3},"spend":{"USD":"0.002000"}}],
		"total":{"requests":3,"input_units":310,"output_units":105,"generated_units":0,"spend":{"USD":"0.031000"}},"truncated":false}`)
	console.on("GET /internal/v1/audit", http.StatusOK, `{"data":[{"id":"0b8f8a52-63a4-4bd4-8c86-5f3a1f0d4e21","ts":"2026-11-19T09:12:44Z","action":"credential.create",
		"actor":"person","actor_person_id":"`+snapshottest.UserPersonID+`","resource_type":"customer_credential","resource_id":"k_9f3a","result":"success","new_status":null},
		{"id":"5c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f","ts":"2026-11-19T09:10:00Z","action":"end_user.update",
		"actor":"person","actor_person_id":"`+snapshottest.UserPersonID+`","resource_type":"end_user","resource_id":"e_1","result":"success","new_status":"suspended"}],"total":2,"page":1,"page_size":20}`)

	dates := page(t, users[snapshottest.UserSub], http.MethodGet, "/api/data/self?default_time=hour&start_timestamp=1795000000", "", GetUserQuotaDates, "/api/data/self")
	require.True(t, dates.Success, dates.raw)
	assert.Contains(t, console.lastCall(t).query, "bucket=hour")
	require.Len(t, dates.List, 3, "one point per Console row, as it is")
	first := dates.List[0].(map[string]any)
	assert.Equal(t, "claude-sonnet", first["model_name"])
	assert.EqualValues(t, 1795082400, first["created_at"])
	assert.EqualValues(t, 2, first["count"])
	assert.EqualValues(t, 500, first["token_used"], "cached and speech tokens count with the rest")
	assert.EqualValues(t, 50, first["cache_read_tokens"], "the dashboard's cache read card")
	assert.EqualValues(t, 30, first["cache_creation_tokens"], "the dashboard's cache write card: both TTLs")
	assert.EqualValues(t, 20, first["units"].(map[string]any)["speech_input_tokens"])
	assert.EqualValues(t, 15, dates.List[1].(map[string]any)["token_used"], "a Console that lists no units: input, output and cache")
	rerank := dates.List[2].(map[string]any)
	assert.EqualValues(t, 0, rerank["token_used"])
	assert.Equal(t, map[string]any{"search_units": float64(3)}, rerank["units"], "a unit new-api has no column for")
	assert.EqualValues(t, int(0.03*common.QuotaPerUnit), first["quota"], "the Console's amount, shown in new-api's units")
	assert.EqualValues(t, int(0.001*common.QuotaPerUnit), dates.List[1].(map[string]any)["quota"], "in the customer's currency")

	page(t, users[snapshottest.UserSub], http.MethodGet, "/api/data/self?default_time=day", "", GetUserQuotaDates, "/api/data/self")
	assert.Contains(t, console.lastCall(t).query, "bucket=day")

	stat := page(t, users[snapshottest.UserSub], http.MethodGet, "/api/log/self/stat", "", GetLogsSelfStat, "/api/log/self/stat")
	require.True(t, stat.Success, stat.raw)
	assert.EqualValues(t, int(0.031*common.QuotaPerUnit), stat.Data["quota"], "the Console's total for the window")

	audit := page(t, users[snapshottest.UserSub], http.MethodGet, "/api/audit/self?p=1&page_size=20", "", GetAuditLogs, "/api/audit/self")
	require.True(t, audit.Success, audit.raw)
	row := audit.Data["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "credential.create", row["action"])
	assert.Equal(t, "customer_credential k_9f3a", row["content"])
	assert.Equal(t, map[string]any{"action": "credential.create", "params": map[string]any{"source": "console"}}, row["other"].(map[string]any)["op"])
	suspended := audit.Data["items"].([]any)[1].(map[string]any)["other"].(map[string]any)["op"]
	assert.Equal(t, map[string]any{"action": "end_user.update", "params": map[string]any{"source": "console", "new_status": "suspended"}}, suspended, "the status the Console's event set")
	assert.Equal(t, "user-1@acme.example", row["username"], "the acting person by email")
	assert.Equal(t, true, row["success"])
	assert.Equal(t, snapshottest.UserPersonID, console.lastCall(t).person)

	logins := page(t, users[snapshottest.UserSub], http.MethodGet, "/api/audit/self?category=login", "", GetAuditLogs, "/api/audit/self")
	assert.Empty(t, logins.Data["items"], "the Console's audit has operations only")
}

func TestTheAPIDocsNameTheGatewaysAddressAndTheReadersModels(t *testing.T) {
	_, users := consolePages(t)
	t.Setenv("ESSERA_PUBLIC_URL", "https://ai-gateway-dev.esseratech.com/")
	openAPI := func(userID int, query ...string) (document map[string]any, raw string) {
		engine := gin.New()
		engine.GET("/api/essera/openapi.json", func(c *gin.Context) { c.Set("id", userID) }, EsseraOpenAPI)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/essera/openapi.json"+strings.Join(query, ""), nil))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &document), "the document is JSON")
		return document, recorder.Body.String()
	}

	admin, raw := openAPI(users[snapshottest.AdminSub])
	servers := admin["servers"].([]any)
	assert.Equal(t, "https://ai-gateway-dev.esseratech.com", servers[0].(map[string]any)["url"])
	paths := admin["paths"].(map[string]any)
	for _, path := range []string{"/v1/models", "/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/embeddings", "/v1/embeddings/tasks", "/v1/embeddings/tasks/{task_id}", "/v1/rerank", "/v1/images/edits", "/v1/realtime"} {
		assert.Contains(t, paths, path)
	}
	assert.Len(t, paths, 10)
	assert.Contains(t, raw, `model=\"claude-sonnet\"`, "the first model the admin may call")
	assert.NotContains(t, raw, apidocs.ExampleChatModel)

	_, raw = openAPI(0)
	assert.Contains(t, raw, `model=\"`+apidocs.ExampleChatModel+`\"`, "an operator keeps the document's examples")

	chinese, raw := openAPI(users[snapshottest.AdminSub], "?lang=zh")
	assert.Equal(t, "Essera AI 网关 API", chinese["info"].(map[string]any)["title"])
	assert.Contains(t, raw, `model=\"claude-sonnet\"`)
	assert.Equal(t, "Essera AI Gateway API", admin["info"].(map[string]any)["title"], "English unless asked")
}

func TestTheLandingReadsThePlatformMetricsOnceAMinuteAndNothingWhileTheConsoleIsDown(t *testing.T) {
	console, _ := consolePages(t)
	console.on("GET /internal/v1/model-metrics/platform", http.StatusOK,
		`{"span":"7d","availability":99.62,"p50_ttft_ms":455,"requests":18450,"checked_at":"2026-11-20T10:04:51Z","computed_at":"2026-11-20T10:05:00Z"}`)
	t.Cleanup(func() { platformMetrics.readAt = time.Time{} })
	platformMetrics.readAt = time.Time{}
	landing := func() map[string]any {
		engine := gin.New()
		engine.GET("/api/essera/platform-metrics", EsseraPlatformMetrics)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/essera/platform-metrics", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		var body map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		return body
	}

	data := landing()["data"].(map[string]any)
	assert.Equal(t, 99.62, data["availability"])
	assert.EqualValues(t, 455, data["p50_ttft_ms"])
	assert.Empty(t, console.lastCall(t).person, "the summary names no person")
	landing()
	assert.Equal(t, 1, console.callCount(), "read once a minute, whoever visits")

	console.server.Close()
	platformMetrics.readAt = time.Now().Add(-2 * time.Minute)
	assert.Nil(t, landing()["data"], "nothing rather than the old figures")
}
