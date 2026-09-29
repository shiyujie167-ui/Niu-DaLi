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
