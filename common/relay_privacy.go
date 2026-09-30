package common

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// PublicRelayFailureMessage keeps upstream diagnostics out of client responses.
func PublicRelayFailureMessage(requestID string) string {
	return MessageWithRequestId("The model request failed. Contact support with the request ID.", requestID)
}

// PublicRelayJSON removes routing/accounting provenance from a client copy.
// Only protocol envelopes are inspected: generated text, tool arguments, image
// data and user metadata must never be traversed or rewritten. Callers retain
// the original upstream bytes for usage accounting and server-side diagnostics.
func PublicRelayJSON(data []byte, requestID string, redactErrors bool) []byte {
	if !gjson.ValidBytes(data) {
		return data
	}
	if gjson.ParseBytes(data).IsArray() {
		items := gjson.ParseBytes(data).Array()
		public := make([]RawMessage, len(items))
		for i, item := range items {
			public[i] = PublicRelayJSON([]byte(item.Raw), requestID, redactErrors)
		}
		if clean, err := Marshal(public); err == nil {
			return clean
		}
		return data
	}
	prefixes := []string{"", "response.", "message."}
	if gjson.GetBytes(data, "data.task_id").Exists() {
		prefixes = append(prefixes, "data.")
	}
	for _, prefix := range prefixes {
		for _, key := range []string{"system_fingerprint", "usage_source", "usage_semantic", "upstream", "upstream_url", "upstream_request_id", "trace_id", "request_id", "provider_metadata", "provider", "provider_name", "channel_id", "channel_name", "usage.usage_source", "usage.usage_semantic", "usage.billing_usage", "usage.cost", "usage.cost_details", "usageMetadata.usage_source", "usageMetadata.usage_semantic", "usageMetadata.provider"} {
			path := prefix + key
			if !gjson.GetBytes(data, path).Exists() {
				continue
			}
			if clean, err := sjson.DeleteBytes(data, path); err == nil {
				data = clean
			}
		}
	}
	for _, prefix := range prefixes {
		for _, field := range []string{"fail_reason", "failReason", "failure_reason", "error_message"} {
			value := gjson.GetBytes(data, prefix+field)
			if !value.Exists() || value.Type == gjson.Null || value.Raw == `""` {
				continue
			}
			if clean, err := sjson.SetBytes(data, prefix+field, PublicRelayFailureMessage(requestID)); err == nil {
				data = clean
			}
		}
	}

	if !redactErrors {
		return data
	}
	// Upstream failures can contain URLs, credentials, HTML and provider names.
	// Keep the protocol's error shape and correlation IDs, not its diagnostic text.
	for _, prefix := range []string{"", "response."} {
		path := prefix + "error"
		upstreamError := gjson.GetBytes(data, path)
		eventType := gjson.GetBytes(data, prefix+"type").String()
		if !strings.Contains(eventType, "error") && (!upstreamError.Exists() || upstreamError.Type == gjson.Null || upstreamError.Raw == `""` || upstreamError.Raw == `{}`) {
			continue
		}
		if !upstreamError.Exists() && eventType != "error" && eventType != "upstream_error" {
			continue
		}
		message := PublicRelayFailureMessage(requestID)
		code := "upstream_error"
		// These protocol codes drive client retry/cancellation behavior.
		switch candidate := upstreamError.Get("code").String(); candidate {
		case "invalid_input", "server_error", "invalid_request", "invalid_request_error", "access_denied", "insufficient_quota", "insufficient_user_quota", "rate_limit_exceeded", "response_not_found", "response_not_active", "response_already_completed", "context_length_exceeded", "model_not_found", "unsupported_parameter":
			code = candidate
		}
		errorType := "api_error"
		switch candidate := upstreamError.Get("type").String(); candidate {
		case "invalid_request_error", "server_error", "rate_limit_error", "authentication_error", "permission_error", "not_found_error", "overloaded_error":
			errorType = candidate
		}
		if !upstreamError.Exists() {
			// Responses SSE uses a flat error event, unlike WebSocket errors.
			if clean, err := sjson.SetBytes(data, prefix+"message", message); err == nil {
				data = clean
			}
			// Keep a standardized error code; never copy diagnostic fields.
			if candidate := gjson.GetBytes(data, prefix+"code").String(); candidate == "server_error" || candidate == "context_length_exceeded" || candidate == "invalid_request_error" {
				code = candidate
			}
			if clean, err := sjson.SetBytes(data, prefix+"code", code); err == nil {
				data = clean
			}
		} else {
			publicError := map[string]any{"code": code, "message": message}
			if upstreamError.Get("type").Exists() {
				publicError["type"] = errorType
			}
			if number := upstreamError.Get("code"); number.Type == gjson.Number && number.Int() >= 400 && number.Int() <= 599 && number.Float() == float64(number.Int()) {
				publicError["code"] = number.Int()
			}
			switch status := upstreamError.Get("status").String(); status {
			case "INVALID_ARGUMENT", "UNAUTHENTICATED", "PERMISSION_DENIED", "NOT_FOUND", "RESOURCE_EXHAUSTED", "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED":
				publicError["status"] = status
			}
			if clean, err := sjson.DeleteBytes(data, prefix+"message"); err == nil {
				data = clean
			}
			errorJSON, err := Marshal(publicError)
			if err != nil {
				continue
			}
			if clean, err := sjson.SetRawBytes(data, path, errorJSON); err == nil {
				data = clean
			}
		}
		for _, field := range []string{"metadata", "debug", "details"} {
			if clean, err := sjson.DeleteBytes(data, prefix+field); err == nil {
				data = clean
			}
		}
	}
	return data
}

// PublicRelaySSE filters complete SSE frames, including multiline data events.
// Comments and event framing are kept; [DONE] is not interpreted as JSON.
func PublicRelaySSE(frame []byte, requestID string) []byte {
	newline := []byte("\n")
	if bytes.Contains(frame, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	lines := bytes.Split(frame, newline)
	var payload []byte
	errorEvent := false
	first := -1
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("event:")) && strings.Contains(strings.ToLower(string(line[6:])), "error") {
			errorEvent = true
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		if first >= 0 {
			payload = append(payload, '\n')
		} else {
			first = i
		}
		payload = append(payload, bytes.TrimPrefix(line[5:], []byte(" "))...)
	}
	if first < 0 {
		return frame
	}
	input := payload
	if errorEvent {
		if !gjson.ValidBytes(input) {
			input = []byte(`{"type":"error"}`)
		} else if clean, err := sjson.SetBytes(input, "type", "error"); err == nil {
			input = clean
		}
	}
	clean := PublicRelayJSON(input, requestID, true)
	if bytes.Equal(clean, payload) {
		return frame
	}
	var out bytes.Buffer
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("data:")) {
			if i != first {
				continue
			}
			out.WriteString("data: ")
			// Sanitization can retain newlines from a multiline upstream JSON object.
			out.Write(bytes.ReplaceAll(clean, []byte("\n"), append(append([]byte{}, newline...), []byte("data: ")...)))
		} else {
			out.Write(line)
		}
		if i < len(lines)-1 {
			out.Write(newline)
		}
	}
	return out.Bytes()
}

// RelayProtocolHeader is an allowlist, so new infrastructure fingerprint headers
// cannot accidentally become public. Gateway-owned CORS/security/request headers
// are applied separately; an upstream must not overwrite them.
func RelayProtocolHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Content-Type", "Content-Disposition", "Content-Encoding", "Cache-Control", "Expires", "Last-Modified", "Etag", "Retry-After", "Content-Range", "Accept-Ranges", "X-Accel-Buffering", "X-Reasoning-Included", "X-Codex-Turn-State":
		return true
	}
	return false
}
