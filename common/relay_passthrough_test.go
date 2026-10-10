package common

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRelayErrorPassthroughFor locks the operator-facing contract of the relay
// error pass-through switch.
//
// Requirements it encodes:
//   - the default forwards upstream errors, because a client cannot fix a call
//     it is not allowed to diagnose;
//   - X-Error-Passthrough is the positive spelling and X-Error-Passthrough
//     wins when both headers are present;
//   - X-Error-No is the negative spelling operators asked for, so "false" (and
//     an absent header) means pass-through;
//   - an unparsable value falls back to the global setting instead of silently
//     switching redaction back on.
func TestRelayErrorPassthroughFor(t *testing.T) {
	previous := RelayErrorPassthrough
	t.Cleanup(func() { RelayErrorPassthrough = previous })

	header := func(values map[string]string) func(string) string {
		headers := http.Header{}
		for name, value := range values {
			headers.Set(name, value)
		}
		return headers.Get
	}

	for _, tc := range []struct {
		name            string
		global          bool
		headers         map[string]string
		wantPassthrough bool
	}{
		{"default is pass-through", true, nil, true},
		{"global redaction is honoured", false, nil, false},
		{"positive header turns it off", true, map[string]string{"X-Error-Passthrough": "false"}, false},
		{"positive header turns it on", false, map[string]string{"X-Error-Passthrough": "true"}, true},
		{"positive header accepts 1", false, map[string]string{"X-Error-Passthrough": "1"}, true},
		{"positive header is case insensitive", true, map[string]string{"x-error-passthrough": "FALSE"}, false},
		{"legacy header true hides errors", true, map[string]string{"X-Error-No": "true"}, false},
		{"legacy header false passes errors through", false, map[string]string{"X-Error-No": "false"}, true},
		{"positive header wins when both are set", true, map[string]string{"X-Error-Passthrough": "true", "X-Error-No": "true"}, true},
		{"unparsable positive header keeps the global setting", true, map[string]string{"X-Error-Passthrough": "maybe"}, true},
		{"unparsable legacy header keeps the global setting", false, map[string]string{"X-Error-No": "sure"}, false},
		{"empty header keeps the global setting", true, map[string]string{"X-Error-Passthrough": ""}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RelayErrorPassthrough = tc.global
			require.Equal(t, tc.wantPassthrough, RelayErrorPassthroughFor(header(tc.headers)))
		})
	}

	t.Run("nil getter reads the global setting", func(t *testing.T) {
		RelayErrorPassthrough = true
		require.True(t, RelayErrorPassthroughFor(nil))
		RelayErrorPassthrough = false
		require.False(t, RelayErrorPassthroughFor(nil))
	})
}

// TestPassthroughRelayJSONKeepsUpstreamDiagnostics separates the two jobs of the
// sanitizer: gateway provenance must never reach a client, upstream error text
// must whenever pass-through is on.
func TestPassthroughRelayJSONKeepsUpstreamDiagnostics(t *testing.T) {
	original := []byte(`{"error":{"type":"invalid_request_error","message":"upstream said no","metadata":{"hint":"keep"}},"channel_name":"private-provider","upstream_url":"https://private.invalid","system_fingerprint":"private-fingerprint","usage":{"cost":0.4},"usage_source":"private-worker"}`)

	clean := PassthroughRelayJSON(original)
	require.Contains(t, string(clean), "upstream said no")
	require.Contains(t, string(clean), "invalid_request_error")
	require.Contains(t, string(clean), "keep")
	for _, leaked := range []string{"private-provider", "private.invalid", "private-fingerprint", `"usage_source"`, `"cost"`} {
		require.NotContains(t, string(clean), leaked)
	}
	// The caller's copy must be untouched so accounting still sees provenance.
	require.Contains(t, string(original), "private-provider")
}

// TestPublicRelayJSONRedactsWhenAsked is the mirror of the test above: the
// privacy policy still has to work when an operator turns pass-through off.
func TestPublicRelayJSONRedactsWhenAsked(t *testing.T) {
	original := []byte(`{"error":{"type":"invalid_request_error","message":"upstream said no"},"channel_name":"private-provider"}`)

	clean := string(PublicRelayJSON(original, "local-request", true))
	require.NotContains(t, clean, "upstream said no")
	require.Contains(t, clean, "local-request")
	require.NotContains(t, clean, "private-provider")
}

// TestRelayProtocolHeader covers the response-header allowlist.
//
// It exists because the list was exact-name only, which cannot express
// Anthropic's rate-limit family: those names carry a per-window suffix, and a
// client has to read them to decide whether to back off. Everything that is not
// on the list stays private, including upstream-chosen credentials and the
// gateway's own provenance.
func TestRelayProtocolHeader(t *testing.T) {
	allowed := []string{
		"Content-Type",
		"Retry-After",
		"X-Accel-Buffering",
		"Anthropic-Ratelimit-Requests-Limit",
		"anthropic-ratelimit-requests-remaining",
		"Anthropic-Ratelimit-Tokens-Reset",
	}
	for _, name := range allowed {
		require.True(t, RelayProtocolHeader(name), "%s must be published", name)
	}

	denied := []string{
		"Authorization",
		"Set-Cookie",
		"Server",
		"Via",
		"X-Oneapi-Request-Id",
		"X-Upstream-Request-Id",
		"Anthropic-Version",
		"Content-Length",
		"",
	}
	for _, name := range denied {
		require.False(t, RelayProtocolHeader(name), "%s must stay private", name)
	}
}

// TestMessageWithRequestIdDecoratesOncePerChain covers the correlation id on the
// public error message, the other half of what a client can see in place of the
// upstream diagnostic.
//
// Relay chains put several gateways between the client and the upstream and each
// hop decorates the same message, so a multi-hop failure used to arrive carrying
// one id per hop instead of the originating one.
func TestMessageWithRequestIdDecoratesOncePerChain(t *testing.T) {
	const message = "The upstream rejected the request."
	decorated := MessageWithRequestId(message, "req-first-hop")
	require.Equal(t, "The upstream rejected the request. (request id: req-first-hop)", decorated)

	// Re-entering the chain with the same or a later hop must not append again.
	require.Equal(t, decorated, MessageWithRequestId(decorated, "req-first-hop"))
	require.Equal(t, decorated, MessageWithRequestId(decorated, "req-second-hop"))
	require.NotContains(t, MessageWithRequestId(decorated, "req-second-hop"), "req-second-hop")

	// A message with no id to add is returned untouched rather than decorated
	// with an empty one.
	require.Equal(t, "plain failure", MessageWithRequestId("plain failure", ""))
}
