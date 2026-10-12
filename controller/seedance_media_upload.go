package controller

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// This endpoint only brokers scoped upload tickets. It never accepts a caller
// supplied upstream URL or exposes the channel key. Normal token/group/model
// authorization and channel selection run before it.
type seedanceMediaUpload struct {
	Model    string `json:"model"`
	Size     int64  `json:"size"`
	MIMEType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
}

type seedanceMediaTicket struct {
	URL       string            `json:"url"`
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt int64             `json:"expires_at"`
}

func PrepareSeedanceMediaUpload(c *gin.Context) {
	var input seedanceMediaUpload
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"message": "Invalid media upload request"}})
		return
	}
	digest, err := base64.RawURLEncoding.DecodeString(input.SHA256)
	supportedMIMEs := []string{"image/png", "image/jpeg", "image/webp", "image/gif", "video/mp4", "video/webm", "video/quicktime", "audio/mpeg", "audio/wav", "audio/ogg", "audio/mp4", "audio/flac"}
	if err != nil || len(digest) != 32 || base64.RawURLEncoding.EncodeToString(digest) != input.SHA256 || input.Size < 1 || input.Size > 100000000 || !slices.Contains(supportedMIMEs, input.MIMEType) {
		c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"message": "Invalid media size, MIME type or SHA-256 digest"}})
		return
	}
	plugin, ok := jsplugin.DefaultRegistry.Get("88api-seedance")
	if !constant.TaskPluginEnabled || !ok || !slices.Contains(plugin.Meta.Models, input.Model) {
		c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"message": "Select an enabled 88API Seedance model before uploading media"}})
		return
	}
	c.Set("expected_task_plugin_key", "88api-seedance")
	c.Set("seedance_media_upload", input)
	c.Next()
}

func SeedanceMediaUpload(c *gin.Context) {
	input, ok := c.MustGet("seedance_media_upload").(seedanceMediaUpload)
	if !ok {
		c.AbortWithStatus(400)
		return
	}
	base, err := url.Parse(common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
	// Tickets are specific to the documented 88API asset service. Do not forward
	// credentials to arbitrary hosts, redirects, or another gateway's asset API.
	if err != nil || base.Scheme != "https" || base.Host != "api.88api.ai" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		c.JSON(400, gin.H{"error": gin.H{"message": "Media uploads require an 88API channel with Base URL https://api.88api.ai"}})
		return
	}
	settings, _ := common.GetContextKeyType[relaydto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		c.JSON(502, gin.H{"error": gin.H{"message": "Media upload service unavailable"}})
		return
	}
	payload, err := common.Marshal(map[string]any{"size": input.Size, "mime_type": input.MIMEType, "sha256": input.SHA256})
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://api.88api.ai/v1/media/uploads", bytes.NewReader(payload))
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	req.Header.Set("Authorization", "Bearer "+common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	req.Header.Set("Content-Type", "application/json")
	ticket, err := requestSeedanceMediaTicket(client, req, input.MIMEType)
	if err != nil {
		// Upstream bodies can contain credentials or signed URLs: return only a
		// generic error; do not echo/log their payload or transport errors.
		c.JSON(502, gin.H{"error": gin.H{"message": "Failed to obtain a valid media upload ticket"}})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, ticket)
}

func requestSeedanceMediaTicket(client *http.Client, req *http.Request, mimeType string) (*seedanceMediaTicket, error) {
	isolated := *client
	isolated.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := isolated.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("media upload rejected")
	}
	var ticket seedanceMediaTicket
	// Same 1 MiB envelope ceiling as task-plugin JSON results; asset bytes never
	// pass through this ticket response.
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("invalid upload response")
	}
	if err = common.Unmarshal(data, &ticket); err != nil {
		return nil, err
	}
	for _, value := range []string{ticket.URL, ticket.UploadURL} {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host != "assets.88api.ai" || parsed.User != nil || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/media/reference-media/") {
			return nil, errors.New("invalid media host")
		}
	}
	if ticket.Method != http.MethodPut || ticket.ExpiresAt <= common.GetTimestamp() {
		return nil, errors.New("invalid upload ticket")
	}
	headers := http.Header{}
	for name, value := range ticket.Headers {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("invalid upload headers")
		}
		headers.Set(name, value)
	}
	if len(headers) != 2 || headers.Get("Content-Type") != mimeType || headers.Get("X-Media-Upload-Token") == "" {
		return nil, errors.New("invalid upload headers")
	}
	ticket.Headers = map[string]string{"Content-Type": mimeType, "X-Media-Upload-Token": headers.Get("X-Media-Upload-Token")}
	return &ticket, nil
}
