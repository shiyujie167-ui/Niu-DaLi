package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newClaudeValidationContext builds a native /v1/messages request context for
// the validator.
func newClaudeValidationContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// TestValidateClaudeRequestEnforcesRolesAndRanges covers the two things a
// native /v1/messages request is refused for: a role no Claude model accepts,
// and a sampling value outside the documented ranges.
//
// Sampling controls a model no longer accepts are deliberately NOT rejected
// here. The converter clears them for such models on every entry point and
// records claude_sampling_removed, so native /v1/messages behaves like
// /v1/chat/completions instead of answering 400 for a request that succeeds on
// the other protocol. See TestApplyReasoning* in
// relaykit/relayconvert/internal/shared/claude.
//
// max_tokens=0 and a missing max_tokens are deliberately NOT errors: the Claude
// adaptor turns both into the per-model default.
func TestValidateClaudeRequestEnforcesRolesAndRanges(t *testing.T) {
	body := func(model, extra string) string {
		return `{"model":"` + model + `","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]` + extra + `}`
	}

	for _, tc := range []struct {
		name    string
		request string
		wantErr string
	}{
		{"strict model baseline", body("claude-opus-5-5", ""), ""},
		{"strict model forwards a temperature the converter will clear", body("claude-opus-5-5", `,"temperature":0.7`), ""},
		{"strict model forwards a top_p the converter will clear", body("claude-opus-5-5", `,"top_p":0.5`), ""},
		{"strict model forwards a top_k the converter will clear", body("claude-opus-5-5", `,"top_k":5`), ""},
		{"strict model still range checks temperature", body("claude-opus-5-5", `,"temperature":2`), "temperature: 2 is out of range"},
		{"strict model still range checks top_p", body("claude-opus-5-5", `,"top_p":1.5`), "top_p: 1.5 is out of range"},
		{"strict model still range checks top_k", body("claude-opus-5-5", `,"top_k":-1`), "top_k: -1 is out of range"},

		{"legacy model accepts temperature 0", body("claude-sonnet-4", `,"temperature":0`), ""},
		{"legacy model rejects temperature above 1", body("claude-sonnet-4", `,"temperature":2`), "temperature: 2 is out of range"},
		{"legacy model rejects negative temperature", body("claude-sonnet-4", `,"temperature":-0.5`), "temperature: -0.5 is out of range"},
		{"legacy model rejects top_p 0", body("claude-sonnet-4", `,"top_p":0`), "top_p: 0 is out of range"},
		{"legacy model accepts top_p 1", body("claude-sonnet-4", `,"top_p":1`), ""},
		{"legacy model accepts top_k 0", body("claude-sonnet-4", `,"top_k":0`), ""},
		{"legacy model rejects negative top_k", body("claude-sonnet-4", `,"top_k":-1`), "top_k: -1 is out of range"},

		{"unknown model falls back to the documented ranges", body("my-claude-proxy", `,"temperature":0.5,"top_p":0.5,"top_k":40`), ""},
		{"unknown model still range checked", body("my-claude-proxy", `,"temperature":5`), "temperature: 5 is out of range"},

		{"ignores fields it does not model", body("claude-opus-5-5", `,"stop_sequences":["x"],"chat_template_kwargs":{"enable_thinking":true},"foo":1`), ""},
		{"rejects a bad role", `{"model":"claude-opus-5-5","max_tokens":1024,"messages":[{"role":"tool","content":"hi"}]}`, "messages.0.role"},
		{"accepts a system role", `{"model":"claude-opus-5-5","max_tokens":1024,"messages":[{"role":"system","content":"hi"},{"role":"user","content":"hi"}]}`, ""},
		{"reports the message index of a bad role", `{"model":"claude-opus-5-5","max_tokens":1024,"messages":[{"role":"user","content":"hi"},{"role":"foo","content":"hi"}]}`, "messages.1.role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetAndValidateClaudeRequest(newClaudeValidationContext(t, tc.request))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestValidateClaudeRequestKeepsMaxTokensNormalization pins the behaviour that
// made strict validation risky: max_tokens=0 means "use the model default", and
// so does omitting it. Rejecting either would break every client that lets the
// gateway pick a budget.
func TestValidateClaudeRequestKeepsMaxTokensNormalization(t *testing.T) {
	t.Run("max_tokens 0 is accepted and preserved for the adaptor", func(t *testing.T) {
		request, err := GetAndValidateClaudeRequest(newClaudeValidationContext(t,
			`{"model":"claude-opus-5-5","max_tokens":0,"messages":[{"role":"user","content":"hi"}]}`))
		require.NoError(t, err)
		require.NotNil(t, request.MaxTokens)
		require.EqualValues(t, 0, *request.MaxTokens)
	})

	t.Run("missing max_tokens is accepted", func(t *testing.T) {
		request, err := GetAndValidateClaudeRequest(newClaudeValidationContext(t,
			`{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`))
		require.NoError(t, err)
		require.Nil(t, request.MaxTokens)
	})
}
