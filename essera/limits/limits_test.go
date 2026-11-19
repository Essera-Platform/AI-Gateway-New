package limits_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const month = "2026-11"

var asOf = time.Date(2026, 11, 19, 10, 0, 0, 500_000_000, time.UTC)

func ptr[T any](v T) *T { return &v }

func customer(entitlements snapshot.Entitlements) []limits.Subject {
	return []limits.Subject{{Scope: "customer", ID: "c-1", Limits: entitlements}}
}

// publish delivers a usage.totals event as the Console sends it.
func publish(t *testing.T, at time.Time, subjects string) {
	t.Helper()
	body := fmt.Sprintf(`{"environment": "test", "month": %q, "timezone": "UTC", "as_of": %q, "subjects": [%s]}`,
		month, at.Format(time.RFC3339Nano), subjects)
	require.NoError(t, limits.ApplyTotals([]byte(body)))
}

func setup(t *testing.T) {
	t.Helper()
	t.Setenv("ESSERA_ENVIRONMENT", "test")
	recordstest.UseDatabase(t)
}

func TestSpendIsTheConsolesFigureInTheAccountsCurrency(t *testing.T) {
	setup(t)
	limit := snapshot.Entitlements{MonthlySpendLimit: ptr(decimal.RequireFromString("50"))}

	publish(t, asOf, `{"scope": "customer", "id": "c-1", "requests": 3, "tokens": 900, "spend": {"USD": "49.999999", "EUR": "80"}}`)
	exceeded, err := limits.Exceeded(customer(limit), month, "USD")
	require.NoError(t, err)
	assert.Empty(t, exceeded, "below the limit in USD; what was billed in EUR is another currency")

	publish(t, asOf.Add(time.Minute), `{"scope": "customer", "id": "c-1", "requests": 4, "tokens": 950, "spend": {"USD": "50.000000"}}`)
	exceeded, err = limits.Exceeded(customer(limit), month, "USD")
	require.NoError(t, err)
	assert.Equal(t, "spend", exceeded)

	publish(t, asOf.Add(2*time.Minute), `{"scope": "customer", "id": "c-1", "requests": 4, "tokens": 950, "spend": {"USD": "40.000000"}}`)
	exceeded, err = limits.Exceeded(customer(limit), month, "USD")
	require.NoError(t, err)
	assert.Empty(t, exceeded, "a re-rating that lowered the spend reopens the account")
}

func TestTheGatewayComputesNoSpendOfItsOwn(t *testing.T) {
	setup(t)
	limit := snapshot.Entitlements{MonthlySpendLimit: ptr(decimal.RequireFromString("0.01"))}
	subjects := customer(limit)

	_, err := limits.Reserve(subjects, month, asOf)
	require.NoError(t, err)
	require.NoError(t, limits.Add(subjects, month, asOf, 50_000_000))

	exceeded, err := limits.Exceeded(subjects, month, "USD")
	require.NoError(t, err)
	assert.Empty(t, exceeded, "with no totals yet the spend is zero, however many tokens were served")
}

func TestRequestsAndTokensAreTheConsolesTotalPlusWhatTheGatewayCountedSinceAsOf(t *testing.T) {
	setup(t)
	quota := snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(5)), MonthlyTokenQuota: ptr(int64(1000))}
	subjects := customer(quota)

	before := asOf.Add(-10 * time.Second)
	sameSecond := asOf.Add(200 * time.Millisecond) // after as_of, but in the second it falls in
	after := asOf.Add(3 * time.Second)
	for _, at := range []time.Time{before, sameSecond} {
		_, err := limits.Reserve(subjects, month, at)
		require.NoError(t, err)
		require.NoError(t, limits.Add(subjects, month, at, 100))
	}
	publish(t, asOf, `{"scope": "customer", "id": "c-1", "requests": 3, "tokens": 700, "spend": {}}`)

	exceeded, err := limits.Exceeded(subjects, month, "USD")
	require.NoError(t, err)
	assert.Empty(t, exceeded, "3 requests and 700 tokens: what was served before as_of is in the Console's total, not counted twice")

	_, err = limits.Reserve(subjects, month, after)
	require.NoError(t, err)
	require.NoError(t, limits.Add(subjects, month, after, 300))
	exceeded, err = limits.Exceeded(subjects, month, "USD")
	require.NoError(t, err)
	assert.Equal(t, "tokens", exceeded, "700 published plus 300 served after as_of")

	exceeded, err = limits.Reserve(subjects, month, after)
	require.NoError(t, err)
	assert.Empty(t, exceeded, "3 published, 1 after as_of, this one is the fifth")
	exceeded, err = limits.Reserve(subjects, month, after)
	require.NoError(t, err)
	assert.Equal(t, "requests", exceeded, "a sixth is refused and not counted")
}

func TestAnOlderPublicationNeverReplacesANewerOne(t *testing.T) {
	setup(t)
	subjects := customer(snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(10))})

	publish(t, asOf, `{"scope": "customer", "id": "c-1", "requests": 10, "tokens": 0, "spend": {}}`)
	publish(t, asOf.Add(-time.Minute), `{"scope": "customer", "id": "c-1", "requests": 2, "tokens": 0, "spend": {}}`)

	exceeded, err := limits.Exceeded(subjects, month, "USD")
	require.NoError(t, err)
	assert.Equal(t, "requests", exceeded)
}

func TestASubjectTheConsoleDidNotListCountsOnlyWhatFollowsTheNewestAsOf(t *testing.T) {
	setup(t)
	endUser := []limits.Subject{{Scope: "end_user", ID: "c-1/u-1", Limits: snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(1))}}}

	_, err := limits.Reserve(endUser, month, asOf.Add(-time.Minute))
	require.NoError(t, err)
	exceeded, err := limits.Exceeded(endUser, month, "USD")
	require.NoError(t, err)
	assert.Equal(t, "requests", exceeded, "no totals yet: the Gateway's own count")

	publish(t, asOf, `{"scope": "customer", "id": "c-1", "requests": 1, "tokens": 0, "spend": {}}`)
	exceeded, err = limits.Exceeded(endUser, month, "USD")
	require.NoError(t, err)
	assert.Empty(t, exceeded, "the Console listed nothing for this end user at as_of, so nothing before it counts")
}

func TestTotalsSurviveARestartAndCountsTheConsoleHasAreDropped(t *testing.T) {
	setup(t)
	subjects := customer(snapshot.Entitlements{MonthlySpendLimit: ptr(decimal.RequireFromString("50")), MonthlyRequestQuota: ptr(int64(3))})
	_, err := limits.Reserve(subjects, month, asOf.Add(-time.Hour))
	require.NoError(t, err)
	_, err = limits.Reserve(subjects, month, asOf.Add(time.Hour))
	require.NoError(t, err)

	publish(t, asOf, `{"scope": "customer", "id": "c-1", "requests": 2, "tokens": 0, "spend": {"USD": "50"}}`)
	var kept []records.LocalUse
	require.NoError(t, model.DB.Find(&kept).Error)
	require.Len(t, kept, 1, "only the count after as_of is still needed")

	recordstest.Restart(t)

	exceeded, err := limits.Exceeded(subjects, month, "USD")
	require.NoError(t, err)
	assert.Equal(t, "requests", exceeded, "2 published plus 1 after as_of, read back after the restart")
	exceeded, err = limits.Exceeded(customer(snapshot.Entitlements{MonthlySpendLimit: ptr(decimal.RequireFromString("50"))}), month, "USD")
	require.NoError(t, err)
	assert.Equal(t, "spend", exceeded)
}

func TestTotalsForAnotherEnvironmentAreRefused(t *testing.T) {
	setup(t)
	err := limits.ApplyTotals([]byte(`{"environment": "prod", "month": "2026-11", "as_of": "2026-11-19T10:00:00Z", "subjects": []}`))
	assert.ErrorContains(t, err, "prod")
}

func TestANewMonthDropsTheLastOnesCounts(t *testing.T) {
	setup(t)
	subjects := customer(snapshot.Entitlements{MonthlyRequestQuota: ptr(int64(1))})
	_, err := limits.Reserve(subjects, "2026-10", asOf.AddDate(0, -1, 0))
	require.NoError(t, err)
	publish(t, asOf, `{"scope": "customer", "id": "c-1", "requests": 0, "tokens": 0, "spend": {}}`)

	var left int64
	require.NoError(t, model.DB.Model(&records.LocalUse{}).Where("month = ?", "2026-10").Count(&left).Error)
	assert.Zero(t, left)
}
