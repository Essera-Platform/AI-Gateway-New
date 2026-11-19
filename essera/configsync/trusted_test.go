package configsync

import (
	"testing"

	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/snapshot/snapshottest"
	"github.com/stretchr/testify/assert"
)

func TestOnlyThisEnvironmentsSnapshotsAndPlausibleVersionsAreTrusted(t *testing.T) {
	t.Setenv("ESSERA_ENVIRONMENT", "dev")
	active := snapshottest.View() // version 7
	event := func(bucket, key string, version int64) snapshot.Event {
		return snapshot.Event{Bucket: bucket, SnapshotKey: key, Version: version}
	}

	assert.NoError(t, trusted(event("essera-dev-config-snapshots-212626318832", "dev/config/v000008.json", 8), active))
	assert.Error(t, trusted(event("attacker-bucket", "dev/config/v000008.json", 8), active))
	assert.Error(t, trusted(event("essera-prod-config-snapshots-1", "dev/config/v000008.json", 8), active))
	assert.Error(t, trusted(event("essera-dev-config-snapshots-1", "elsewhere/v000008.json", 8), active))
	assert.Error(t, trusted(event("essera-dev-config-snapshots-1", "dev/config/v9e18.json", 9_000_000_000), active), "a version far ahead would freeze every real one")
	assert.NoError(t, trusted(event("essera-dev-config-snapshots-1", "dev/config/v1.json", 9_000_000_000), nil), "the first version applied may be any")
}

func TestTheQueueCarriesConfigurationVersionsAndUsageTotals(t *testing.T) {
	totals := `{"detail-type": "usage.totals", "source": "essera.backend", "detail": {"environment": "dev", "month": "2026-11"}}`
	detailType, detail := decodeMessage(totals)
	assert.Equal(t, "usage.totals", detailType)
	assert.JSONEq(t, `{"environment": "dev", "month": "2026-11"}`, string(detail))

	for _, body := range []string{
		`{"detail-type": "configuration.updated", "detail": {"version": 8, "environment": "dev"}}`,
		`{"version": 8, "environment": "dev"}`,
	} {
		event, err := decodeEvent(body)
		assert.NoError(t, err)
		assert.Equal(t, int64(8), event.Version, body)
	}
}
