package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitRuntimeBrandDefaults(t *testing.T) {
	originalSystemName := SystemName
	originalLogo := Logo
	originalFooter := Footer
	t.Cleanup(func() {
		SystemName = originalSystemName
		Logo = originalLogo
		Footer = originalFooter
	})

	t.Run("uses environment overrides", func(t *testing.T) {
		SystemName = originalSystemName
		Logo = originalLogo
		Footer = originalFooter
		SystemName = "New API"
		Logo = ""
		Footer = ""
		t.Setenv("SYSTEM_NAME", "Niu Dali")
		t.Setenv("SYSTEM_LOGO", "/niu-dali-logo.svg")
		t.Setenv("SYSTEM_FOOTER", "Niu Dali")

		initRuntimeBrandDefaults()

		require.Equal(t, "Niu Dali", SystemName)
		require.Equal(t, "/niu-dali-logo.svg", Logo)
		require.Equal(t, "Niu Dali", Footer)
	})

	t.Run("keeps built-in defaults for empty values", func(t *testing.T) {
		SystemName = originalSystemName
		Logo = originalLogo
		Footer = originalFooter
		SystemName = "New API"
		Logo = ""
		Footer = ""
		t.Setenv("SYSTEM_NAME", "")
		t.Setenv("SYSTEM_LOGO", "")
		t.Setenv("SYSTEM_FOOTER", "")

		initRuntimeBrandDefaults()

		require.Equal(t, "New API", SystemName)
		require.Empty(t, Logo)
		require.Empty(t, Footer)
	})
}

// TestRelayResponseHeaderTimeout pins the published non-stream budget. The
// gateway advertises a 60-minute non-stream timeout, so the default has to stay
// at one hour rather than the 30 minutes it used to be; a silent regression
// would truncate long Claude reasoning calls for every client.
func TestRelayResponseHeaderTimeout(t *testing.T) {
	require.Equal(t, 3600, DefaultRelayResponseHeaderTimeout)

	t.Run("environment override wins", func(t *testing.T) {
		t.Setenv("RELAY_RESPONSE_HEADER_TIMEOUT", "120")
		require.Equal(t, 120, GetEnvOrDefault("RELAY_RESPONSE_HEADER_TIMEOUT", DefaultRelayResponseHeaderTimeout))
	})

	t.Run("unset environment keeps one hour", func(t *testing.T) {
		t.Setenv("RELAY_RESPONSE_HEADER_TIMEOUT", "")
		require.Equal(t, 3600, GetEnvOrDefault("RELAY_RESPONSE_HEADER_TIMEOUT", DefaultRelayResponseHeaderTimeout))
	})

	t.Run("unparsable environment keeps one hour", func(t *testing.T) {
		t.Setenv("RELAY_RESPONSE_HEADER_TIMEOUT", "soon")
		require.Equal(t, 3600, GetEnvOrDefault("RELAY_RESPONSE_HEADER_TIMEOUT", DefaultRelayResponseHeaderTimeout))
	})
}

// TestRelayErrorPassthroughDefault pins the shipped default: errors pass
// through unless an operator turns redaction back on.
func TestRelayErrorPassthroughDefault(t *testing.T) {
	require.True(t, RelayErrorPassthrough)
	t.Run("is overridable from the environment", func(t *testing.T) {
		t.Setenv("RELAY_ERROR_PASSTHROUGH", "false")
		require.False(t, GetEnvOrDefaultBool("RELAY_ERROR_PASSTHROUGH", true))
	})
}
