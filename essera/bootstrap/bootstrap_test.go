package bootstrap

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/records/recordstest"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAManagedGatewayIsNamedForEssera(t *testing.T) {
	recordstest.UseDatabase(t, &model.Option{}, &model.User{}, &model.Token{})
	previousName, previousLogo, previousMaster := common.SystemName, common.Logo, common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.SystemName, common.Logo, common.IsMasterNode = previousName, previousLogo, previousMaster
	})
	model.InitOptionMap()

	require.NoError(t, Run())
	assert.Equal(t, "Essera AI Gateway", common.SystemName)
	assert.Equal(t, previousLogo, common.Logo, "new-api's logo stays unless one is configured")

	t.Setenv("ESSERA_SYSTEM_NAME", "Helios AI")
	t.Setenv("ESSERA_LOGO", "https://cdn.example/essera.svg")
	require.NoError(t, Run())
	assert.Equal(t, "Helios AI", common.SystemName)
	assert.Equal(t, "https://cdn.example/essera.svg", common.Logo)
	var stored model.Option
	require.NoError(t, model.DB.Where("key = ?", "SystemName").First(&stored).Error)
	assert.Equal(t, "Helios AI", stored.Value, "kept in the options every node loads")
}
