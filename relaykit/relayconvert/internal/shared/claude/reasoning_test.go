package claude

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claudeRequestWithSampling builds a native /v1/messages body carrying every
// sampling control the strict-sampling models reject.
func claudeRequestWithSampling(model string) *dto.ClaudeRequest {
	temperature, topP, topK := 0.7, 0.5, 5
	return &dto.ClaudeRequest{
		Model:       model,
		Temperature: &temperature,
		TopP:        &topP,
		TopK:        &topK,
	}
}

// applyNative runs the native /v1/messages path (no host modifier, no suffix)
// and returns the diagnostics the converter recorded.
func applyNative(t *testing.T, req *dto.ClaudeRequest, meta convmeta.Meta) []types.ConversionDiagnostic {
	t.Helper()
	ctx, collector := convdiag.WithCollector(context.Background())
	require.NoError(t, ApplyReasoning(ctx, req, meta, reasoning.Intent{}, false))
	return collector.Diagnostics()
}

// TestApplyReasoningClearsSamplingOnNativeStrictModel pins the fix for the
// asymmetry a resale channel hit in production: the same request succeeded
// through /v1/chat/completions, where the converter already drops sampling
// controls, and failed through native /v1/messages, where it forwarded them to
// an upstream that had deprecated temperature. Both entry points must render
// the model the same way.
func TestApplyReasoningClearsSamplingOnNativeStrictModel(t *testing.T) {
	req := claudeRequestWithSampling("claude-opus-5-5")

	diagnostics := applyNative(t, req, nil)

	assert.Nil(t, req.Temperature)
	assert.Nil(t, req.TopP)
	assert.Nil(t, req.TopK)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "claude_sampling_removed", diagnostics[0].Code)
	assert.Equal(t, types.RelayFormat(types.RelayFormatClaude), diagnostics[0].To)
}

// TestApplyReasoningKeepsSamplingForLegacyModel is the counterweight: a model
// that still accepts sampling controls must receive them untouched.
func TestApplyReasoningKeepsSamplingForLegacyModel(t *testing.T) {
	req := claudeRequestWithSampling("claude-sonnet-4")

	diagnostics := applyNative(t, req, nil)

	require.NotNil(t, req.Temperature)
	require.NotNil(t, req.TopP)
	require.NotNil(t, req.TopK)
	assert.Equal(t, 0.7, *req.Temperature)
	assert.Empty(t, diagnostics)
}

// TestApplyReasoningLeavesRequestAloneWhenNothingToClear keeps the diagnostic
// honest: an empty sampling block must not be reported as a removal.
func TestApplyReasoningLeavesRequestAloneWhenNothingToClear(t *testing.T) {
	req := &dto.ClaudeRequest{Model: "claude-opus-5-5"}

	diagnostics := applyNative(t, req, nil)

	assert.Empty(t, diagnostics)
}

// TestApplyReasoningKeepsSamplingWhenChannelOptsOut covers the per-channel
// escape hatch for a Claude-compatible proxy that implements its own sampling
// rules and does accept these controls.
func TestApplyReasoningKeepsSamplingWhenChannelOptsOut(t *testing.T) {
	req := claudeRequestWithSampling("claude-opus-5-5")
	meta := &convmeta.Values{Options: &convmeta.Options{
		Claude: convmeta.ClaudeOptions{AllowNativeSampling: true},
	}}

	diagnostics := applyNative(t, req, meta)

	require.NotNil(t, req.Temperature)
	require.NotNil(t, req.TopP)
	require.NotNil(t, req.TopK)
	assert.Empty(t, diagnostics)
}

// TestApplyReasoningClearsSamplingOnCrossProtocolPath guards the behaviour the
// native path was aligned with, so the two cannot drift apart again.
func TestApplyReasoningClearsSamplingOnCrossProtocolPath(t *testing.T) {
	req := claudeRequestWithSampling("claude-opus-5-5")
	ctx, collector := convdiag.WithCollector(context.Background())

	require.NoError(t, ApplyReasoning(ctx, req, nil, reasoning.Intent{}, true))

	assert.Nil(t, req.Temperature)
	assert.Nil(t, req.TopP)
	assert.Nil(t, req.TopK)
	require.Len(t, collector.Diagnostics(), 1)
	assert.Equal(t, "claude_sampling_removed", collector.Diagnostics()[0].Code)
}
