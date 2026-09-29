package middleware_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay/channel/minimax"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func privacyRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(common.RequestIdKey, "local-request")
		c.Header(common.RequestIdKey, "local-request")
		c.Header("Access-Control-Allow-Origin", "https://customer.example")
	})
	engine.POST("/test", middleware.RouteTag("relay"), handler)
	return engine
}

func TestRelayPrivacyAcrossProtocols(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"chat", `{"id":"completion-id","model":"customer-model","choices":[{"message":{"content":"usage_source private-worker"}}],"system_fingerprint":"private-fingerprint","usage":{"total_tokens":10,"usage_source":"private-worker","usage_semantic":"estimated","cost":0.4,"billing_usage":{"source":"private-provider"}}}`},
		{"responses", `{"type":"response.completed","response":{"id":"response-id","system_fingerprint":"private-fingerprint","usage":{"input_tokens":10,"usage_source":"private-worker"}},"output":[{"text":"usage_source private-worker"}]}`},
		{"claude", `{"type":"message_start","message":{"id":"message-id","usage":{"input_tokens":10,"usage_source":"private-worker"}},"content":[{"text":"usage_source private-worker"}]}`},
		{"gemini-array", `[{"provider":"private-provider","candidates":[{"content":{"parts":[{"text":"usage_source private-worker"}]}}],"usageMetadata":{"totalTokenCount":10}}]`},
		{"task", `{"id":"public-task-id","status":"failed","fail_reason":"private-failure","channel_name":"private-provider","upstream_url":"https://private.invalid","data":{"text":"usage_source private-worker"}}`},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				var original string
				engine := privacyRouter(func(c *gin.Context) {
					c.Header("X-Railway-Edge", "private-platform")
					c.Writer.Header()["x-lowercase-provider"] = []string{"private-lowercase"}
					c.Header("X-Hikari-Trace", "private-trace")
					c.Header("X-New-Api-Version", "private-version")
					c.Header("X-Unknown-Provider", "private-unknown")
					c.Header("Server", "private-server")
					c.Header("Via", "private-proxy")
					c.Header("Set-Cookie", "private-cookie")
					c.Header("Access-Control-Allow-Origin", "https://private.invalid")
					c.Header(common.RequestIdKey, "private-request")
					c.Header("Retry-After", "3")
					original = tc.body
					if stream {
						c.Header("Content-Type", "text/event-stream")
						_, err := c.Writer.WriteString(": keepalive\n\nevent: response\ndata: " + tc.body[:len(tc.body)/2])
						require.NoError(t, err)
						c.Writer.Flush()
						_, err = c.Writer.WriteString(tc.body[len(tc.body)/2:] + "\n\ndata: [DONE]\n\n")
						require.NoError(t, err)
						c.Writer.Flush()
					} else {
						c.Header("Content-Type", "application/json")
						c.Header("Content-Length", fmt.Sprint(len(tc.body)))
						_, err := c.Writer.WriteString(tc.body[:len(tc.body)/2])
						require.NoError(t, err)
						c.Writer.Flush()
						_, err = c.Writer.WriteString(tc.body[len(tc.body)/2:])
						require.NoError(t, err)
					}
				})
				recorder := httptest.NewRecorder()
				engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/test", nil))
				assert.Equal(t, http.StatusOK, recorder.Code)
				assert.Equal(t, tc.body, original)
				for _, field := range []string{`"usage_source"`, `"usage_semantic"`, `"billing_usage"`, `"cost"`, `"system_fingerprint"`, `"provider"`, `"channel_name"`, `"upstream_url"`} {
					assert.NotContains(t, recorder.Body.String(), field)
				}
				assert.Contains(t, recorder.Body.String(), "usage_source private-worker", "generated content must be untouched")
				assert.NotContains(t, recorder.Body.String(), "private-failure")
				for _, name := range []string{"X-Railway-Edge", "X-Hikari-Trace", "X-New-Api-Version", "X-Unknown-Provider", "Server", "Via", "Set-Cookie"} {
					assert.Empty(t, recorder.Header().Get(name))
				}
				assert.NotContains(t, recorder.Header(), "x-lowercase-provider")
				assert.Equal(t, "local-request", recorder.Header().Get(common.RequestIdKey))
				assert.Equal(t, "https://customer.example", recorder.Header().Get("Access-Control-Allow-Origin"))
				assert.Equal(t, "3", recorder.Header().Get("Retry-After"))
				if stream {
					assert.Contains(t, recorder.Body.String(), "event: response")
					assert.Contains(t, recorder.Body.String(), "data: [DONE]")
				} else {
					assert.Equal(t, fmt.Sprint(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
				}
			})
		}
	}
}

func TestRelayPrivacyKeepsImageAccountingAndBinaryMedia(t *testing.T) {
	const body = `{"data":[{"b64_json":"original-image"}],"usage":{"input_tokens":19,"output_tokens":1056,"total_tokens":1075,"usage_source":"private-worker","usage_semantic":"estimated"}}`
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			engine := privacyRouter(func(c *gin.Context) {
				response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}, common.RequestIdKey: []string{"upstream-request"}}, Body: io.NopCloser(strings.NewReader(body))}
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, IsStream: stream}
				if stream {
					usage, err := openai.OpenaiImageStreamHandler(c, info, response)
					require.Nil(t, err)
					require.NotNil(t, usage)
					assert.Equal(t, "private-worker", usage.UsageSource)
					assert.Equal(t, 1056, usage.CompletionTokens)
				} else {
					usage, err := openai.OpenaiImageHandler(c, info, response)
					require.Nil(t, err)
					require.NotNil(t, usage)
					assert.Equal(t, "private-worker", usage.UsageSource)
					assert.Equal(t, 1056, usage.CompletionTokens)
					assert.Equal(t, "upstream-request", c.GetString(common.UpstreamRequestIdKey))
				}
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
			assert.NotContains(t, recorder.Body.String(), "private-worker")
			assert.NotContains(t, recorder.Body.String(), "usage_semantic")
			assert.Contains(t, recorder.Body.String(), "original-image")
			assert.Contains(t, recorder.Body.String(), `"output_tokens":1056`)
		})
	}
	engine := privacyRouter(func(c *gin.Context) { c.Data(200, "audio/mpeg", []byte("audio\x00\xffprivate-worker")) })
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
	assert.Equal(t, []byte("audio\x00\xffprivate-worker"), recorder.Body.Bytes())
	for _, name := range []string{"Content-Type", "Retry-After", "X-Reasoning-Included", "X-Codex-Turn-State"} {
		assert.True(t, service.ShouldCopyUpstreamHeader(nil, name, []string{"value"}))
	}
	assert.False(t, service.ShouldCopyUpstreamHeader(nil, "X-New-Api-Version", []string{"value"}))
	assert.False(t, service.ShouldCopyUpstreamHeader(nil, "Content-Type", nil))
}

func TestRelayPrivacyHidesErrorDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		status            int
		contentType, body string
	}{
		{502, "application/json", `{"error":{"message":"Post https://private.invalid: secret","code":"private-provider","metadata":{"key":"secret"}},"debug":"private-worker"}`},
		{429, "text/html", "<html>private-provider secret</html>"},
		{200, "text/event-stream", "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"response_not_found\",\"message\":\"private-provider secret\"}}\n\n"},
		{200, "application/json", "{broken private-provider secret"},
	} {
		t.Run(fmt.Sprint(tc.status)+tc.contentType, func(t *testing.T) {
			engine := privacyRouter(func(c *gin.Context) { c.Data(tc.status, tc.contentType, []byte(tc.body)) })
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
			assert.NotContains(t, recorder.Body.String(), "private-")
			assert.NotContains(t, recorder.Body.String(), "secret")
			assert.Contains(t, recorder.Body.String(), "local-request")
			if tc.contentType == "text/event-stream" {
				assert.Contains(t, recorder.Body.String(), "response_not_found")
			} else if tc.status >= 400 {
				assert.Equal(t, tc.status, recorder.Code)
			} else {
				assert.Equal(t, 502, recorder.Code)
			}
		})
	}
	// WebSocket callers use the same sanitizer on a client-only copy.
	original := []byte(`{"type":"response.done","response":{"id":"resume-id","usage":{"total_tokens":7,"usage_source":"private-worker"}}}`)
	clean := common.PublicRelayJSON(original, "request", false)
	assert.NotContains(t, string(clean), "private-worker")
	assert.Contains(t, string(clean), "resume-id")
	assert.Contains(t, string(original), "private-worker")
	multiline := []byte("event: message\r\ndata: {\"usage\":\r\ndata: {\"total_tokens\":7,\"usage_source\":\"private-worker\"}}\r\n\r\n")
	result := common.PublicRelaySSE(multiline, "request")
	assert.NotContains(t, string(result), "private-worker")
	assert.Contains(t, string(result), "event: message")
	assert.Contains(t, string(result), `"total_tokens":7`)
}

func TestRelayPrivacyProxiesAudioWithoutDisclosingTheDownloadHost(t *testing.T) {
	fetch := system_setting.GetFetchSetting()
	previous := *fetch
	t.Cleanup(func() { *fetch = previous; service.InitHttpClient() })
	fetch.AllowPrivateIp = true
	fetch.AllowedPorts = []string{"1-65535"}
	service.InitHttpClient()
	audio := []byte("audio\x00\xff")
	downloads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/audio", http.StatusFound)
			return
		}
		w.Header().Set("X-Private-Upstream", "hidden")
		_, _ = w.Write(audio)
	}))
	defer upstream.Close()
	engine := privacyRouter(func(c *gin.Context) {
		body, err := common.Marshal(map[string]any{"data": map[string]any{"audio": upstream.URL + "/redirect"}, "extra_info": map[string]any{"usage_characters": 5}})
		require.NoError(t, err)
		response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, RelayMode: relayconstant.RelayModeAudioSpeech}
		_, apiErr := (&minimax.Adaptor{}).DoResponse(c, response, info)
		if !fetch.AllowPrivateIp {
			require.NotNil(t, apiErr)
			c.JSON(502, gin.H{"error": apiErr.ToOpenAIError()})
			return
		}
		require.Nil(t, apiErr)
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
	assert.Equal(t, 200, recorder.Code)
	assert.Equal(t, audio, recorder.Body.Bytes())
	assert.Equal(t, 2, downloads)
	assert.Empty(t, recorder.Header().Get("Location"))
	assert.Empty(t, recorder.Header().Get("X-Private-Upstream"))
	fetch.AllowPrivateIp = false
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
	assert.Equal(t, 502, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), upstream.URL)
	assert.Equal(t, 2, downloads, "private addresses must be blocked before fetching")
}

func TestRelayPrivacyLeavesLocalValidationAndPricingIntact(t *testing.T) {
	engine := privacyRouter(func(c *gin.Context) {
		c.JSON(400, gin.H{"error": gin.H{"type": "new_api_error", "code": "invalid_request", "message": "n must be between 1 and 128"}})
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
	assert.Contains(t, recorder.Body.String(), "n must be between 1 and 128")
	engine = gin.New()
	engine.GET("/api/pricing", middleware.RouteTag("api"), func(c *gin.Context) {
		c.JSON(200, gin.H{"billing_expr": "customer-price-expression", "group_ratio": 1})
	})
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/pricing", nil))
	assert.JSONEq(t, `{"billing_expr":"customer-price-expression","group_ratio":1}`, recorder.Body.String())
	for _, frame := range []string{"event: error\ndata: private-worker secret\n\n", "event: error\ndata: {\"message\":\"private-worker secret\"}\n\n"} {
		clean := common.PublicRelaySSE([]byte(frame), "local-request")
		assert.NotContains(t, string(clean), "private-worker")
		assert.NotContains(t, string(clean), "secret")
		assert.Contains(t, string(clean), "local-request")
	}
	engine = privacyRouter(func(c *gin.Context) {
		c.Header("Content-Type", "audio/mpeg")
		_, err := c.Writer.Write([]byte("partial-audio"))
		require.NoError(t, err)
		c.Writer.Flush()
		c.JSON(502, gin.H{"error": gin.H{"message": "https://private.invalid"}})
	})
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/test", nil))
	assert.Equal(t, "partial-audio", recorder.Body.String())
}
