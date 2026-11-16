package policy_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func useDatabase(t *testing.T) { recordstest.UseDatabase(t) }

func caller(s *snapshot.Snapshot, clientID string) *auth.Caller {
	view := snapshot.NewView(7, s)
	credential := view.Credential(clientID)
	customer := view.Customer(credential.CustomerID)
	return &auth.Caller{ClientID: clientID, Credential: credential, Customer: customer, Partner: view.Partner(customer.PartnerID), View: view}
}

func TestAServableRequestGetsItsModelsAndProviders(t *testing.T) {
	useDatabase(t)

	request, denial := policy.Evaluate(caller(snapshottest.Sample(), snapshottest.ClientID), "user-1", "US", now)

	require.Nil(t, denial)
	assert.Equal(t, map[string][]string{
		"gpt-4o":        {"essera/openai"}, // allowed on openai only, though bedrock serves it too
		"claude-sonnet": {"essera/bedrock"},
	}, request.AllowedModels, "gpt-old is disabled; acme-llm is not entitled")
	assert.Equal(t, "USD", request.Currency)
	assert.Equal(t, "2026-11", request.Month)
	require.NotNil(t, request.EndUser)
	assert.Equal(t, int64(2), *request.Effective.MonthlyRequestQuota, "the end user's own limit narrows the customer's")
	assert.Len(t, request.Subjects, 4, "partner, customer, credential and end user")
}

func TestADirectCustomerWithoutRestrictionsGetsEveryServableModel(t *testing.T) {
	useDatabase(t)
	s := snapshottest.Sample()
	s.Regions = snapshot.Regions{}

	request, denial := policy.Evaluate(caller(s, snapshottest.DirectClientID), "", "", now)

	require.Nil(t, denial)
	assert.ElementsMatch(t, []string{"essera/openai", "essera/bedrock"}, request.AllowedModels["gpt-4o"])
	assert.Contains(t, request.AllowedModels, "acme-1")
	assert.NotContains(t, request.AllowedModels, "gpt-old")
}

func TestDenials(t *testing.T) {
	useDatabase(t)
	maintenance := snapshottest.Sample()
	end := now.Add(10 * time.Minute)
	maintenance.Gateway = snapshot.GatewaySection{MaintenanceStatus: snapshot.MaintenanceActive, Message: "Back at noon.", WindowEnd: &end}
	suspendedPartner := snapshottest.Sample()
	suspendedPartner.Accounts.Partners[0].Status = "suspended"

	cases := []struct {
		name     string
		snapshot *snapshot.Snapshot
		endUser  string
		country  string
		status   int
		code     string
	}{
		{"maintenance", maintenance, "", "US", http.StatusServiceUnavailable, "essera_maintenance"},
		{"suspended partner", suspendedPartner, "", "US", http.StatusForbidden, "essera_account_inactive"},
		{"suspended end user", snapshottest.Sample(), "user-off", "US", http.StatusForbidden, "essera_account_inactive"},
		{"country outside the account", snapshottest.Sample(), "", "GB", http.StatusForbidden, "essera_country_denied"},
		{"unknown country", snapshottest.Sample(), "", "", http.StatusForbidden, "essera_country_unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, denial := policy.Evaluate(caller(tc.snapshot, snapshottest.ClientID), tc.endUser, tc.country, now)
			assert.Nil(t, request)
			require.NotNil(t, denial)
			assert.Equal(t, tc.status, denial.Status)
			assert.Equal(t, tc.code, denial.Code)
		})
	}
	_, denial := policy.Evaluate(caller(maintenance, snapshottest.ClientID), "", "US", now)
	assert.Equal(t, "Back at noon.", denial.Message)
	assert.Equal(t, "601", denial.Headers["Retry-After"])
}

func TestScheduledMaintenanceIsAnnouncedNotEnforced(t *testing.T) {
	useDatabase(t)
	s := snapshottest.Sample()
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	s.Gateway = snapshot.GatewaySection{MaintenanceStatus: snapshot.MaintenanceScheduled, WindowStart: &start, WindowEnd: &end}

	request, denial := policy.Evaluate(caller(s, snapshottest.ClientID), "", "US", now)

	require.Nil(t, denial)
	assert.Equal(t, "scheduled; start=2026-11-13T13:00:00Z; end=2026-11-13T14:00:00Z", request.Headers["X-Essera-Maintenance"])
}

func TestMonthlyLimitsCountEachLevel(t *testing.T) {
	useDatabase(t)
	s := snapshottest.Sample()

	for range 2 {
		request, denial := policy.Evaluate(caller(s, snapshottest.ClientID), "user-1", "US", now)
		require.Nil(t, denial)
		require.NoError(t, limits.Add(request.Subjects, request.Month, request.Currency, 100, decimal.RequireFromString("1.5")))
	}
	_, denial := policy.Evaluate(caller(s, snapshottest.ClientID), "user-1", "US", now)
	require.NotNil(t, denial, "the end user's two requests are used up")
	assert.Equal(t, http.StatusTooManyRequests, denial.Status)
	assert.Equal(t, "essera_quota_exceeded", denial.Code)

	_, denial = policy.Evaluate(caller(s, snapshottest.ClientID), "", "US", now)
	assert.Nil(t, denial, "the customer itself still has room")

	request, _ := policy.Evaluate(caller(s, snapshottest.ClientID), "", "US", now)
	require.NoError(t, limits.Add(request.Subjects, request.Month, request.Currency, 0, decimal.RequireFromString("47")))
	_, denial = policy.Evaluate(caller(s, snapshottest.ClientID), "", "US", now)
	require.NotNil(t, denial, "50.00 spent of the customer's 50.00")
	assert.Contains(t, denial.Message, "spend")

	_, denial = policy.Evaluate(caller(s, snapshottest.ClientID), "", "US", now.AddDate(0, 1, 0))
	assert.Nil(t, denial, "a new month starts from zero")
}
