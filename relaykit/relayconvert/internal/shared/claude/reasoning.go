package claude

import (
	"context"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func ApplyReasoning(ctx context.Context, req *dto.ClaudeRequest, info convmeta.Meta, source reasoning.Intent, crossProtocol bool) error {
	if req == nil {
		return nil
	}

	opts := convmeta.OptionsOf(info)
	baseModel := req.Model
	capabilityModel := baseModel
	// Resolve the capability model up front so the native fast path below can
	// consult it as well: a mapped upstream name may not be a Claude model,
	// while the client-facing name still is.
	if info != nil && !reasoning.IsKnownClaudeModel(capabilityModel) && reasoning.IsKnownClaudeModel(info.GetOriginModelName()) {
		capabilityModel = info.GetOriginModelName()
	}
	suffix := reasoning.IntentFromState(convmeta.ReasoningStateOf(info))
	preserveSuffix := opts.ShouldPreserveThinkingSuffix(req.Model)
	if info != nil && opts.ShouldPreserveThinkingSuffix(info.GetOriginModelName()) {
		preserveSuffix = true
	}
	if preserveSuffix {
		suffix = reasoning.Intent{}
	}
	// A native Claude request without a host modifier is already in the target
	// protocol, including Claude-compatible proxies that keep native controls
	// instead of applying Anthropic model rules. Read portable effort for
	// accounting metadata, but do not run the capability renderer or rewrite
	// provider-native controls.
	//
	// Sampling controls are the one exception. A provider that dropped them
	// rejects the request outright: AWS Bedrock answers a native /v1/messages
	// carrying temperature with "ValidationException: `temperature` is
	// deprecated for this model", and nothing between this gateway and the
	// provider can repair it. The cross-protocol paths already clear those
	// controls for such models (ClaudeRender.ClearSampling), so forwarding
	// them here would make one model fail through /v1/messages while the same
	// request succeeds through /v1/chat/completions. Channels fronting a proxy
	// that implements its own sampling rules opt out with
	// Options.Claude.AllowNativeSampling.
	if !crossProtocol && source.IsEmpty() && suffix.IsEmpty() {
		if !opts.Claude.AllowNativeSampling && reasoning.ClaudeRequiresStrictSampling(capabilityModel) {
			clearClaudeSampling(ctx, req, types.ConversionDiagnostic{
				Code:     "claude_sampling_removed",
				Path:     "temperature/top_p/top_k",
				Message:  fmt.Sprintf("model %q does not accept sampling controls; they were removed from the native request", capabilityModel),
				Severity: types.ConversionDiagnosticWarning,
				From:     types.RelayFormatClaude,
				To:       types.RelayFormatClaude,
			})
		}
		if info != nil {
			effort := req.GetEfforts()
			if effort == "" && req.Thinking != nil {
				switch {
				case req.Thinking.Type == "disabled":
					effort = string(reasoning.EffortNone)
				case req.Thinking.BudgetTokens != nil:
					effort = string(reasoning.EffortFromBudget(*req.Thinking.BudgetTokens))
				case req.Thinking.Type == "enabled" || req.Thinking.Type == "adaptive":
					effort = string(reasoning.EffortHigh)
				}
			}
			info.SetReasoningEffort(effort)
		}
		return nil
	}

	native, diagnostics, err := reasoning.FromClaude(req)
	if err != nil {
		return err
	}
	convdiag.Add(ctx, diagnostics...)
	explicit, diagnostics, err := reasoning.MergeExplicit(native, source, req.Model)
	if err != nil {
		return err
	}
	convdiag.Add(ctx, diagnostics...)
	intent, diagnostics, err := reasoning.MergeExplicitAndSuffix(explicit, suffix, req.Model)
	if err != nil {
		return err
	}
	convdiag.Add(ctx, diagnostics...)
	knownClaudeModel := reasoning.IsKnownClaudeModel(capabilityModel)
	if !knownClaudeModel && intent.Mode == reasoning.ModeAdaptive {
		// Cross-protocol pivots cannot safely assume that an unknown
		// Claude-compatible model implements Anthropic's adaptive mode. Render
		// the broadly supported manual form while retaining the requested
		// strength. Native Claude requests took the passthrough path above.
		intent.Mode = reasoning.ModeEnabled
		if intent.Effort == "" {
			intent.Effort = reasoning.EffortHigh
		}
	}
	if req.MaxTokens == nil && intent.HasStrength() {
		// Adapter-provided defaults may be raised to accommodate an exact
		// cross-protocol budget. Values too small for manual thinking are
		// raised again by the renderer, which reports the change.
		minimum := uint(1280)
		if configuredDefault, configured := opts.Claude.DefaultMaxTokensFor(capabilityModel); configured && configuredDefault > 0 {
			minimum = uint(configuredDefault)
		}
		if reasoning.ClaudeUsesManualThinking(capabilityModel, intent) && *intent.BudgetTokens >= 0 {
			if *intent.BudgetTokens == math.MaxInt {
				return fmt.Errorf("thinking budget is too large to derive max_tokens")
			}
			required := uint(*intent.BudgetTokens) + 1
			const maxDerivedTokens = uint(math.MaxInt32 / 2)
			if required > maxDerivedTokens {
				return fmt.Errorf("thinking budget %d exceeds the supported conversion limit", *intent.BudgetTokens)
			}
			if minimum < required {
				minimum = required
			}
		}
		req.MaxTokens = &minimum
	}

	rendered, err := reasoning.RenderClaude(capabilityModel, intent, req.MaxTokens, opts.Claude.ThinkingAdapterBudgetTokensPercentage)
	if err != nil {
		return err
	}
	convdiag.Add(ctx, rendered.Diagnostics...)
	req.Model = baseModel
	if rendered.MaxTokens != nil {
		req.MaxTokens = rendered.MaxTokens
	}
	if rendered.Thinking != nil {
		req.Thinking = rendered.Thinking
	}
	if rendered.OutputEffort != "" {
		outputConfig := make(map[string]any)
		if len(req.OutputConfig) > 0 {
			if kitutil.GetJsonType(req.OutputConfig) != "object" {
				return fmt.Errorf("Claude output_config must be a JSON object")
			}
			if err := kitutil.Unmarshal(req.OutputConfig, &outputConfig); err != nil {
				return fmt.Errorf("invalid Claude output_config: %w", err)
			}
			if outputConfig == nil {
				outputConfig = make(map[string]any)
			}
		}
		outputConfig["effort"] = string(rendered.OutputEffort)
		encoded, err := kitutil.Marshal(outputConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal Claude output_config: %w", err)
		}
		req.OutputConfig = encoded
	}
	if rendered.ClearSampling {
		clearClaudeSampling(ctx, req, types.ConversionDiagnostic{
			Code:     "claude_sampling_removed",
			Path:     "temperature/top_p/top_k",
			Message:  fmt.Sprintf("model %q does not accept sampling controls with the selected thinking mode", capabilityModel),
			Severity: types.ConversionDiagnosticWarning,
			From:     types.RelayFormatClaude,
			To:       types.RelayFormatClaude,
		})
	} else if rendered.ConstrainThinkingSampling {
		removedSampling := req.Temperature != nil || req.TopK != nil || req.TopP != nil && (*req.TopP < 0.95 || *req.TopP > 1)
		req.Temperature = nil
		req.TopK = nil
		if req.TopP != nil && (*req.TopP < 0.95 || *req.TopP > 1) {
			req.TopP = nil
		}
		if removedSampling {
			convdiag.Add(ctx, types.ConversionDiagnostic{
				Code:     "claude_sampling_constrained",
				Path:     "temperature/top_p/top_k",
				Message:  fmt.Sprintf("model %q accepts only top_p between 0.95 and 1 with manual thinking", capabilityModel),
				Severity: types.ConversionDiagnosticWarning,
				To:       types.RelayFormatClaude,
			})
		}
	}
	if info != nil && rendered.EffectiveEffort != "" {
		info.SetReasoningEffort(string(rendered.EffectiveEffort))
	}
	return nil
}

// clearClaudeSampling drops temperature / top_p / top_k and records why.
//
// A model whose provider no longer accepts sampling controls must not receive
// them: the provider rejects the whole request, and when the gateway sits
// behind resale layers the client sees an error raised several hops away that
// it cannot act on. The diagnostic keeps the removal visible in the request
// log rather than silently changing what the caller asked for.
func clearClaudeSampling(ctx context.Context, req *dto.ClaudeRequest, diagnostic types.ConversionDiagnostic) {
	if req == nil || (req.Temperature == nil && req.TopP == nil && req.TopK == nil) {
		return
	}
	convdiag.Add(ctx, diagnostic)
	req.Temperature = nil
	req.TopP = nil
	req.TopK = nil
}
