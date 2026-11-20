package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatusReturnsEffectiveOIDCDisplayName(t *testing.T) {
	settings := system_setting.GetOIDCSettings()
	originalDisplayName := settings.DisplayName
	originalOptionMap := common.OptionMap
	t.Cleanup(func() {
		settings.DisplayName = originalDisplayName
		common.OptionMap = originalOptionMap
	})
	common.OptionMap = map[string]string{}

	tests := []struct {
		name        string
		displayName string
		want        string
	}{
		{
			name:        "custom name is trimmed",
			displayName: "  Acme SSO  ",
			want:        "Acme SSO",
		},
		{
			name:        "whitespace-only name falls back",
			displayName: "   ",
			want:        "OIDC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings.DisplayName = tt.displayName
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

			GetStatus(context)

			var payload struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			require.True(t, payload.Success)
			assert.Equal(t, tt.want, payload.Data["oidc_display_name"])
		})
	}
}

// Modified by Essera, 2026-11: the admin UI reads whether the Console administers the gateway.
func TestGetStatusSaysWhetherTheEsseraConsoleManagesTheGateway(t *testing.T) {
	originalOptionMap := common.OptionMap
	t.Cleanup(func() { common.OptionMap = originalOptionMap })
	common.OptionMap = map[string]string{}

	for _, managed := range []bool{false, true} {
		if managed {
			t.Setenv("ESSERA_MANAGED", "true")
		}
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

		GetStatus(context)

		var payload struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
		assert.Equal(t, managed, payload.Data["essera_managed"])
	}
}

// Modified by Essera, 2026-11: a managed gateway shows its public address, not the locked option.
func TestGetStatusGivesAManagedGatewaysPublicAddress(t *testing.T) {
	originalOptionMap, originalAddress := common.OptionMap, system_setting.ServerAddress
	t.Cleanup(func() { common.OptionMap, system_setting.ServerAddress = originalOptionMap, originalAddress })
	common.OptionMap = map[string]string{}
	system_setting.ServerAddress = "http://localhost:3000"

	tests := []struct {
		name       string
		managed    string
		publicURL  string
		trustedURL string
		want       string
	}{
		{name: "not managed keeps the option", managed: "false", trustedURL: "https://gateway.example.com", want: "http://localhost:3000"},
		{name: "managed without an address keeps the option", managed: "true", want: "http://localhost:3000"},
		{name: "managed takes the trusted session URL", managed: "true", trustedURL: "https://gateway.example.com/, https://other.example.com", want: "https://gateway.example.com"},
		{name: "managed prefers ESSERA_PUBLIC_URL", managed: "true", publicURL: " https://api.example.com/ ", trustedURL: "https://gateway.example.com", want: "https://api.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ESSERA_MANAGED", tt.managed)
			t.Setenv("ESSERA_PUBLIC_URL", tt.publicURL)
			t.Setenv("SESSION_COOKIE_TRUSTED_URL", tt.trustedURL)
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

			GetStatus(context)

			var payload struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.Equal(t, tt.want, payload.Data["server_address"])
		})
	}
}
