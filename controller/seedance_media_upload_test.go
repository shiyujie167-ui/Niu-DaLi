package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seedanceTicketTransport func(*http.Request) (*http.Response, error)

func (fn seedanceTicketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSeedanceMediaTicketRejectsUnsafeResponses(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*seedanceMediaTicket)
		valid  bool
	}{
		{"valid scoped ticket", func(*seedanceMediaTicket) {}, true},
		{"other upload host", func(v *seedanceMediaTicket) { v.UploadURL = "https://evil.example/upload" }, false},
		{"private result", func(v *seedanceMediaTicket) { v.URL = "http://127.0.0.1/private" }, false},
		{"asset host credentials", func(v *seedanceMediaTicket) { v.UploadURL = "https://user@assets.88api.ai/media/reference-media/a" }, false},
		{"expired ticket", func(v *seedanceMediaTicket) { v.ExpiresAt = 1 }, false},
		{"unexpected method", func(v *seedanceMediaTicket) { v.Method = "POST" }, false},
		{"credential header", func(v *seedanceMediaTicket) { v.Headers["Authorization"] = "Bearer upstream-secret" }, false},
		{"MIME mismatch", func(v *seedanceMediaTicket) { v.Headers["Content-Type"] = "text/html" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ticket := seedanceMediaTicket{URL: "https://assets.88api.ai/media/reference-media/a.png", UploadURL: "https://assets.88api.ai/media/reference-media/a.png", Method: "PUT", ExpiresAt: time.Now().Add(time.Minute).Unix(), Headers: map[string]string{"Content-Type": "image/png", "X-Media-Upload-Token": "scoped-token"}}
			tc.mutate(&ticket)
			data, err := common.Marshal(ticket)
			require.NoError(t, err)
			client := &http.Client{Transport: seedanceTicketTransport(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "Bearer fixture-upstream-key", req.Header.Get("Authorization"))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), Request: req}, nil
			})}
			req, err := http.NewRequest(http.MethodPost, "https://api.88api.ai/v1/media/uploads", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer fixture-upstream-key")
			result, err := requestSeedanceMediaTicket(client, req, "image/png")
			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, "scoped-token", result.Headers["X-Media-Upload-Token"])
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSeedanceMediaTicketDoesNotFollowRedirect(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: seedanceTicketTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://evil.example/"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "https://api.88api.ai/v1/media/uploads", nil)
	require.NoError(t, err)
	_, err = requestSeedanceMediaTicket(client, req, "image/png")
	require.Error(t, err)
	assert.Equal(t, 1, calls)
}

func TestSeedanceMediaUploadValidatesBeforeChannelSelection(t *testing.T) {
	for _, body := range []string{
		`{"model":"seedance-2.5官方版","size":0,"mime_type":"image/png","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`,
		`{"model":"seedance-2.5官方版","size":100000001,"mime_type":"image/png","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`,
		`{"model":"seedance-2.5官方版","size":1,"mime_type":"text/html","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`,
		`{"model":"unknown","size":1,"mime_type":"image/png","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`,
		`{"model":"seedance-2.5官方版","size":1,"mime_type":"image/png","sha256":"invalid"}`,
	} {
		router := gin.New()
		reached := false
		router.POST("/v1/media/uploads", PrepareSeedanceMediaUpload, func(c *gin.Context) { reached = true; c.Status(200) })
		req := httptest.NewRequest("POST", "/v1/media/uploads", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		assert.Equal(t, 400, recorder.Code)
		assert.False(t, reached)
	}
}

func TestSeedanceMediaUploadRequiresPluginChannel(t *testing.T) {
	router := gin.New()
	router.POST("/v1/media/uploads", PrepareSeedanceMediaUpload, func(c *gin.Context) {
		assert.Equal(t, "88api-seedance", c.GetString("expected_task_plugin_key"))
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://evil.example")
		SeedanceMediaUpload(c)
	})
	req := httptest.NewRequest("POST", "/v1/media/uploads", strings.NewReader(`{"model":"seedance-2.5官方版","size":1,"mime_type":"image/png","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	assert.Equal(t, 400, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "fixture-upstream-key")
}
