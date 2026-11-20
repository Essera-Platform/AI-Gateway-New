// Essera's test of what a managed gateway closes (CHANGES-ESSERA.md, essera/managed): the real
// API routes behind the managed-mode guard, called by a signed-in person and by an operator.
package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/managed"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type managedCaller struct {
	engine *gin.Engine
	token  string
}

func (m managedCaller) call(method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if m.token != "" {
		request.Header.Set("Authorization", "Bearer "+m.token)
	}
	recorder := httptest.NewRecorder()
	m.engine.ServeHTTP(recorder, request)
	return recorder
}

// managedGateway serves the API routes as a managed gateway does, with a person (role user,
// linked in essera_people) and an operator (root) signed in.
func managedGateway(t *testing.T) (person, operator, guest managedCaller) {
	t.Helper()
	t.Setenv("ESSERA_MANAGED", "true")
	recordstest.UseDatabase(t, &model.User{}, &model.UserSession{}, &model.AuditLog{}, &model.UserAccessToken{},
		&model.Option{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.AuthFlow{}, &model.UserOAuthBinding{}, &model.Token{})
	previousLogDB, previousMain, previousLog := model.LOG_DB, common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousSecret := common.RedisEnabled, common.SessionSecret
	model.LOG_DB = model.DB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	if model.DB.Dialector.Name() == "postgres" {
		common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	}
	common.RedisEnabled, common.SessionSecret = false, "essera-managed-router-test"
	t.Cleanup(func() {
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.SessionSecret = previousRedis, previousSecret
	})
	require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))

	signedIn := func(username string, role int, method string) string {
		user := model.User{Username: username, Email: username, Role: role, Status: common.UserStatusEnabled, AffCode: username, AuthVersion: 1}
		require.NoError(t, model.DB.Create(&user).Error)
		if role < common.RoleRootUser {
			require.NoError(t, model.DB.Create(&records.PersonUser{UserID: user.Id, PersonID: "person-" + username, Role: "user", UpdatedAt: time.Now()}).Error)
		}
		bundle, err := service.CreateLoginSession(user.Id, method, "203.0.113.7", "essera-managed-test")
		require.NoError(t, err)
		return bundle.AccessToken
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(managed.Guard())
	SetApiRouter(engine)
	SetDashboardRouter(engine)
	return managedCaller{engine, signedIn("user-1@acme.example", common.RoleCommonUser, "essera")},
		managedCaller{engine, signedIn("root", common.RoleRootUser, "password")},
		managedCaller{engine: engine}
}

func notAvailable(t *testing.T, recorder *httptest.ResponseRecorder, route string) {
	t.Helper()
	assert.Equal(t, http.StatusNotFound, recorder.Code, "%s: %s", route, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "essera_not_available", route)
}

func TestAManagedGatewayClosesAPersonsAccountSettingsButNotAnOperators(t *testing.T) {
	person, operator, _ := managedGateway(t)
	settingBody := `{"notify_type":"email","record_ip_log":true,"accept_unset_model_ratio_model":false}`
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/user/access_tokens", ""},
		{http.MethodGet, "/api/user/access_tokens/catalog", ""},
		{http.MethodGet, "/api/user/passkey", ""},
		{http.MethodGet, "/api/user/2fa/status", ""},
		{http.MethodGet, "/api/user/oauth/bindings", ""},
		{http.MethodPut, "/api/user/setting", settingBody},
	}
	for _, route := range routes {
		name := route.method + " " + route.path
		notAvailable(t, person.call(route.method, route.path, route.body), "a person: "+name)

		recorder := operator.call(route.method, route.path, route.body)
		assert.Equal(t, http.StatusOK, recorder.Code, "an operator: %s: %s", name, recorder.Body.String())
		assert.NotContains(t, recorder.Body.String(), "essera_not_available", "an operator: "+name)
	}
	notAvailable(t, person.call(http.MethodGet, "/api/verify/methods?scope=password_change", ""), "a person: step-up verification")

	var stored model.User
	require.NoError(t, model.DB.Where("username = ?", "user-1@acme.example").First(&stored).Error)
	assert.False(t, stored.GetSetting().RecordIpLog, "a person's settings are not written")

	deleted := person.call(http.MethodDelete, "/api/user/self", "")
	assert.Equal(t, http.StatusConflict, deleted.Code, "closing an account is the Console's")
	for _, path := range []string{"/api/user/sessions", "/api/token/", "/api/status"} {
		assert.Equal(t, http.StatusOK, person.call(http.MethodGet, path, "").Code, "a person keeps %s", path)
	}
}

func TestAManagedGatewayServesNobodyTheFeaturesItDoesNotOffer(t *testing.T) {
	person, operator, guest := managedGateway(t)
	closed := []struct{ method, path string }{
		{http.MethodGet, "/api/about"},
		{http.MethodGet, "/api/notice"},
		{http.MethodGet, "/api/uptime/status"},
		{http.MethodGet, "/api/rankings"},
		{http.MethodGet, "/api/ratio_config"},
		{http.MethodGet, "/api/reset_password?email=root@example.com"},
		{http.MethodGet, "/api/verification?email=root@example.com"},
		{http.MethodGet, "/api/oauth/github"},
		{http.MethodGet, "/api/user/topup/info"},
		{http.MethodGet, "/api/user/topup/self"},
		{http.MethodGet, "/api/user/aff"},
		{http.MethodGet, "/api/user/checkin"},
		{http.MethodGet, "/api/subscription/self"},
		{http.MethodGet, "/api/subscription/admin/plans"},
		{http.MethodGet, "/api/redemption/"},
		{http.MethodGet, "/api/mj/self"},
		{http.MethodGet, "/api/task/self"},
		{http.MethodGet, "/api/user/topup"},
		{http.MethodPost, "/api/stripe/webhook"},
		{http.MethodGet, "/api/usage/token/"},
		{http.MethodGet, "/api/log/token"},
	}
	for _, route := range closed {
		name := route.method + " " + route.path
		for who, caller := range map[string]managedCaller{"a guest": guest, "a person": person, "an operator": operator} {
			notAvailable(t, caller.call(route.method, route.path, ""), who+": "+name)
		}
	}
}

func TestAManagedGatewaysStatusOffersNoChatLinksDocsOrOAuth(t *testing.T) {
	_, _, guest := managedGateway(t)
	recorder := guest.call(http.MethodGet, "/api/status", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Empty(t, body.Data["chats"])
	assert.Empty(t, body.Data["docs_link"])
	assert.Equal(t, false, body.Data["announcements_enabled"])
	assert.Equal(t, false, body.Data["uptime_kuma_enabled"])
	assert.Equal(t, false, body.Data["github_oauth"])
	assert.Equal(t, true, body.Data["essera_managed"])
}
