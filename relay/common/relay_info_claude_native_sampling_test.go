package common

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvOptionsCarriesChannelNativeSamplingPreference pins the host half of
// the native Claude sampling opt-out. A channel that fronts a Claude-compatible
// proxy implementing its own sampling rules sets allow_claude_native_sampling;
// the converter must see it through Options.Claude.AllowNativeSampling,
// otherwise the channel cannot keep temperature on native /v1/messages.
func TestConvOptionsCarriesChannelNativeSamplingPreference(t *testing.T) {
	require.False(t, (&RelayInfo{ChannelMeta: &ChannelMeta{}}).ConvOptions().Claude.AllowNativeSampling)

	optedOut := &RelayInfo{ChannelMeta: &ChannelMeta{
		ChannelOtherSettings: dto.ChannelOtherSettings{AllowClaudeNativeSampling: true},
	}}
	assert.True(t, optedOut.ConvOptions().Claude.AllowNativeSampling)
}

// TestConvOptionsWithoutChannelMetaLeavesSamplingUntouched covers requests with
// no channel attached: the default must stay "clear for strict models", which
// is the safe direction when nothing is known about the upstream.
func TestConvOptionsWithoutChannelMetaLeavesSamplingUntouched(t *testing.T) {
	assert.False(t, (&RelayInfo{}).ConvOptions().Claude.AllowNativeSampling)
}
