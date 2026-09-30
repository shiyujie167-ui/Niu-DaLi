package controller

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

const videoWorkspaceMaxPromptLength = 4000

var videoWorkspaceSubmissions sync.Map

type videoWorkspaceModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	SupportsImage       bool     `json:"supports_image"`
	Durations           []int    `json:"durations,omitempty"`
	Sizes               []string `json:"sizes,omitempty"`
	Resolutions         []string `json:"resolutions,omitempty"`
	DefaultResolution   string   `json:"default_resolution,omitempty"`
	MaxImageBytes       int64    `json:"max_image_bytes,omitempty"`
	MaxPromptLength     int      `json:"max_prompt_length"`
	SupportedImageTypes []string `json:"supported_image_types,omitempty"`
	resolutionMetadata  bool
}

// Session identity and origin checks are separate from API token authentication:
// this surface spends the signed-in user's wallet, without minting an API key.
func VideoWorkspaceSession(c *gin.Context) {
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok || c.GetBool("use_access_token") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "A signed-in browser session is required"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Next()
}

// videoWorkspaceModels deliberately supports only verified built-in protocol
// adapters. A plugin protocol declaration alone does not describe UI inputs.
func videoWorkspaceModels(c *gin.Context) ([]videoWorkspaceModel, error) {
	items := make([]videoWorkspaceModel, 0)
	generation := jsplugin.DefaultRegistry.Generation()
	if generation == nil {
		return items, nil
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	var abilities []model.Ability
	if err := model.DB.Where(map[string]any{"group": group, "enabled": true}).Find(&abilities).Error; err != nil {
		return nil, err
	}
	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var channels []model.Channel
	if len(channelIDs) > 0 {
		if err := model.DB.Where("id IN ? AND status = ?", channelIDs, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
			return nil, err
		}
	}
	channelByID := make(map[int]*model.Channel, len(channels))
	for i := range channels {
		channelByID[channels[i].Id] = &channels[i]
	}
	seen := make(map[string]bool)
	blocked := make(map[string]bool)
	for _, ability := range abilities {
		bindings := generation.LookupEndpointCandidates(http.MethodPost, "/v1/videos", ability.Model)
		// Shared-model plugins can have incompatible parameter contracts. Until
		// they expose a common UI schema, keep these out of this workspace.
		if len(bindings) != 1 || bindings[0].Plugin == nil {
			continue
		}
		plugin := bindings[0].Plugin
		if expression, configured := billing_setting.GetPluginBillingExpr(plugin.Meta.Key, ability.Model); configured {
			schema, _ := plugin.Meta.UsageForModels(ability.Model)
			if !billing_setting.TaskExprCompatible(expression, schema) {
				continue
			}
		} else if !helper.HasModelBillingConfig(ability.Model) {
			continue
		}
		filters := []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: "/v1/videos"}, {
			Kind: dto.FilterTaskPluginIdentity, TaskPluginKey: plugin.Meta.Key, TaskPluginChannelTypes: plugin.Meta.ChannelTypes,
		}}
		channel := channelByID[ability.ChannelId]
		if ok, _ := model.ChannelSatisfiesFilters(channel, ability.Model, filters); !ok {
			continue
		}
		// Mapped models may require different parameters than their displayed
		// name. Preserve routing semantics by not guessing their capabilities.
		if channel.ModelMapping != nil && strings.TrimSpace(*channel.ModelMapping) != "" && strings.TrimSpace(*channel.ModelMapping) != "{}" {
			var mapping map[string]string
			if common.UnmarshalJsonStr(*channel.ModelMapping, &mapping) != nil || mapping[ability.Model] != "" && mapping[ability.Model] != ability.Model {
				blocked[ability.Model] = true
				continue
			}
		}
		if channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != "" && strings.TrimSpace(*channel.ParamOverride) != "{}" {
			blocked[ability.Model] = true
			continue
		}
		if seen[ability.Model] {
			continue
		}
		item := videoWorkspaceModel{ID: ability.Model, Name: ability.Model, MaxPromptLength: videoWorkspaceMaxPromptLength}
		switch plugin.Meta.Key {
		case "sora":
			if !slices.Contains([]string{"sora-2", "sora-2-pro"}, item.ID) {
				continue
			}
			item.SupportsImage, item.MaxImageBytes = true, 10<<20
			item.Durations = []int{4, 8, 12}
			item.Sizes = []string{"720x1280", "1280x720"}
			if item.ID == "sora-2-pro" {
				item.Sizes = append(item.Sizes, "1792x1024", "1024x1792")
			}
		case "kling":
			if !slices.Contains([]string{"kling-v1", "kling-v1-6", "kling-v2-master"}, item.ID) {
				continue
			}
			item.SupportsImage, item.MaxImageBytes = true, 10<<20
			item.Durations = []int{5, 10}
		case "jimeng":
			if item.ID != "jimeng_vgfm_t2v_l20" {
				continue
			}
			item.SupportsImage, item.MaxImageBytes = true, 4928307
			item.Durations = []int{5}
		case "mengwuxian":
			maxDuration := 15
			item.DefaultResolution = "720p"
			switch item.ID {
			case "Sd-2.0满血933":
				item.Resolutions = []string{"720p", "1080p", "4k"}
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 5000)
			case "Sd-2.0fast":
				item.Resolutions = []string{"720p", "1080p"}
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 5000)
			case "Sd-2.5":
				item.Resolutions = []string{"480p", "720p", "1080p"}
				maxDuration = 30
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 15000)
			case "Sd-2.0mini":
				item.Resolutions = []string{"480p", "720p"}
				item.DefaultResolution = "480p"
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 2000)
			default:
				// minmax-h3 requires a reference image; wan-3.0 is unavailable.
				continue
			}
			item.SupportsImage, item.MaxImageBytes = true, 10<<20
			for seconds := 4; seconds <= maxDuration; seconds++ {
				item.Durations = append(item.Durations, seconds)
			}
			item.Sizes = []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}
			if item.ID == "Sd-2.0mini" {
				item.Sizes = []string{"16:9", "9:16", "1:1"}
			}
		case "doubao":
			// Keep the text-capable models from the built-in VIDEO_MODELS table.
			// lite-i2v requires an image, but this adapter accepts only reference
			// URLs, which the workspace intentionally does not expose.
			if !slices.Contains([]string{
				"doubao-seedance-1-0-pro-250528", "doubao-seedance-1-0-lite-t2v", "doubao-seedance-1-5-pro-251215",
				"doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128", "doubao-seedance-2-0-mini-260615", "doubao-seedance-2-5-260628",
			}, item.ID) {
				continue
			}
			item.Resolutions = []string{"480p", "720p", "1080p"}
			switch item.ID {
			case "doubao-seedance-2-0-260128":
				item.Resolutions = append(item.Resolutions, "4k")
			case "doubao-seedance-2-0-fast-260128", "doubao-seedance-2-0-mini-260615":
				item.Resolutions = []string{"480p", "720p"}
			}
			item.resolutionMetadata = true
		case "alibaba":
			// Match the built-in WAN_MODELS table, including supported snapshots.
			// Its resolution tiers are distinct from pixel sizes and ratios.
			item.DefaultResolution = "1080P"
			switch item.ID {
			case "wan2.7-t2v", "wan2.7-t2v-2026-04-25", "wan2.7-t2v-2026-06-12", "wan2.6-t2v", "wan2.6-t2v-us":
				item.Resolutions = []string{"720P", "1080P"}
			case "wan2.5-t2v-preview":
				item.Resolutions = []string{"480P", "720P", "1080P"}
			case "wan2.2-t2v-plus":
				item.Resolutions = []string{"480P", "1080P"}
			case "wanx2.1-t2v-plus":
				item.Resolutions = []string{"720P"}
				item.DefaultResolution = "720P"
			case "wanx2.1-t2v-turbo":
				item.Resolutions = []string{"480P", "720P"}
				item.DefaultResolution = "720P"
			default:
				continue
			}
		default:
			continue
		}
		if item.SupportsImage {
			item.SupportedImageTypes = []string{"image/png", "image/jpeg", "image/webp"}
		}
		items = append(items, item)
		seen[item.ID] = true
	}
	items = slices.DeleteFunc(items, func(item videoWorkspaceModel) bool { return blocked[item.ID] })
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func GetVideoWorkspaceModels(c *gin.Context) {
	items, err := videoWorkspaceModels(c)
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to load video models")
		return
	}
	quota, err := model.GetUserQuota(c.GetInt("id"), false)
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to load balance")
		return
	}
	common.ApiSuccess(c, gin.H{"models": items, "quota": quota})
}

// PrepareVideoWorkspaceSubmission validates the small workspace contract and
// then enters the existing host protocol pipeline once, including its billing
// session, durable task insert, background polling, and failure settlement.
func PrepareVideoWorkspaceSubmission(c *gin.Context) {
	userID := c.GetInt("id")
	if _, loaded := videoWorkspaceSubmissions.LoadOrStore(userID, struct{}{}); loaded {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": gin.H{"message": "A video submission is already in progress. Check your history before submitting again."}})
		return
	}
	defer videoWorkspaceSubmissions.Delete(userID)

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (10<<20)+(64<<10))
	fields := make(map[string]string)
	var upload *multipart.FileHeader
	switch c.ContentType() {
	case gin.MIMEMultipartPOSTForm:
		if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid or oversized video request"}})
			return
		}
		defer c.Request.MultipartForm.RemoveAll()
		for key, values := range c.Request.MultipartForm.Value {
			if len(values) != 1 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Each video parameter must be provided once"}})
				return
			}
			fields[key] = values[0]
		}
		for key, files := range c.Request.MultipartForm.File {
			if key != "input_reference" || len(files) != 1 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Only one reference image is supported"}})
				return
			}
			upload = files[0]
		}
	case gin.MIMEJSON:
		var input map[string]any
		if err := common.DecodeJson(c.Request.Body, &input); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid video request"}})
			return
		}
		for key, value := range input {
			switch value := value.(type) {
			case string:
				fields[key] = value
			case float64:
				if key == "seconds" {
					fields[key] = strconv.FormatFloat(value, 'f', -1, 64)
					continue
				}
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid video parameter type"}})
				return
			default:
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid video parameter type"}})
				return
			}
		}
	default:
		c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": gin.H{"message": "Use JSON or multipart form data"}})
		return
	}
	for key := range fields {
		if !slices.Contains([]string{"model", "prompt", "seconds", "size", "resolution"}, key) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Unsupported video parameter: " + key}})
			return
		}
	}
	prompt := strings.TrimSpace(fields["prompt"])
	if prompt == "" || !utf8.ValidString(prompt) || utf8.RuneCountInString(prompt) > videoWorkspaceMaxPromptLength {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Prompt must contain 1 to 4000 characters"}})
		return
	}
	items, err := videoWorkspaceModels(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "Failed to load video models"}})
		return
	}
	index := slices.IndexFunc(items, func(item videoWorkspaceModel) bool { return item.ID == fields["model"] })
	if index < 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Video model is unavailable. An administrator must configure an enabled video channel and model pricing for your group."}})
		return
	}
	selected := items[index]
	if utf8.RuneCountInString(prompt) > selected.MaxPromptLength {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Prompt exceeds the selected model limit"}})
		return
	}
	if raw, exists := fields["seconds"]; exists {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 || seconds > relaycommon.MaxTaskDurationSeconds || !slices.Contains(selected.Durations, seconds) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Unsupported video duration"}})
			return
		}
	}
	if size, exists := fields["size"]; exists && !slices.Contains(selected.Sizes, size) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Unsupported video size"}})
		return
	}
	if resolution, exists := fields["resolution"]; exists && !slices.Contains(selected.Resolutions, resolution) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Unsupported video resolution"}})
		return
	}
	var imageBytes []byte
	var imageType string
	if upload != nil {
		if !selected.SupportsImage || upload.Size <= 0 || upload.Size > selected.MaxImageBytes {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Reference image is unsupported or exceeds the model upload limit"}})
			return
		}
		file, err := upload.Open()
		if err == nil {
			imageBytes, err = io.ReadAll(io.LimitReader(file, selected.MaxImageBytes+1))
			_ = file.Close()
		}
		if err != nil || int64(len(imageBytes)) > selected.MaxImageBytes {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Failed to read reference image"}})
			return
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(imageBytes))
		imageType = "image/" + format
		if err != nil || !slices.Contains(selected.SupportedImageTypes, imageType) || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Upload a valid PNG, JPEG, or WebP image up to 40 megapixels"}})
			return
		}
	}
	fields["prompt"] = prompt
	var normalized bytes.Buffer
	writer := multipart.NewWriter(&normalized)
	for _, key := range []string{"model", "prompt", "seconds", "size", "resolution"} {
		if value, exists := fields[key]; exists {
			if key == "resolution" && selected.resolutionMetadata {
				// Doubao's video adapter forwards native options from metadata.
				// Construct only the validated field; callers cannot send metadata.
				metadata, err := common.Marshal(map[string]string{"resolution": value})
				if err != nil {
					c.AbortWithStatus(http.StatusInternalServerError)
					return
				}
				_ = writer.WriteField("metadata", string(metadata))
				continue
			}
			_ = writer.WriteField(key, value)
		}
	}
	if imageBytes != nil {
		headers := make(textproto.MIMEHeader)
		headers.Set("Content-Disposition", `form-data; name="input_reference"; filename="reference.`+strings.TrimPrefix(imageType, "image/")+`"`)
		headers.Set("Content-Type", imageType)
		part, err := writer.CreatePart(headers)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		_, _ = part.Write(imageBytes)
	}
	_ = writer.Close()
	c.Request.Body = io.NopCloser(bytes.NewReader(normalized.Bytes()))
	c.Request.ContentLength = int64(normalized.Len())
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request.MultipartForm, c.Request.PostForm, c.Request.Form = nil, nil, nil
	c.Request.URL.Path, c.Request.RequestURI = "/v1/videos", "/v1/videos"
	c.Set(constant.ContextKeyVideoWorkspace, true)
	c.Set("video_workspace_prompt", prompt)
	setting, _ := common.GetContextKeyType[kitdto.UserSetting](c, constant.ContextKeyUserSetting)
	setting.BillingPreference = "wallet_only"
	common.SetContextKey(c, constant.ContextKeyUserSetting, setting)
	group := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
	if err := middleware.SetupContextForToken(c, &model.Token{UserId: userID, Name: "video-workspace", Group: group, UnlimitedQuota: true}); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	// Dashboard authentication must never be copied into upstream headers.
	c.Request.Header.Del("Authorization")
	// Once validated, a browser refresh must not cancel an accepted upstream
	// submission before it is durably recorded and settled.
	submissionContext, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), defaultPluginProtocolBridgeDeps().submissionTimeout)
	defer cancel()
	c.Request = c.Request.Clone(submissionContext)
	c.Next()
}

func GetVideoWorkspaceTasks(c *gin.Context) {
	page := common.GetPageQuery(c)
	page.Page = max(1, page.Page)
	page.PageSize = max(1, min(100, page.PageSize))
	page.Page = min(page.Page, int(^uint(0)>>1)/page.PageSize)
	query := model.DB.Model(&model.Task{}).Where("user_id = ? AND action IN ?", c.GetInt("id"), []string{
		constant.TaskActionTextToVideo, constant.TaskActionImageToVideo, constant.TaskActionFirstTailToVideo, constant.TaskActionReferenceToVideo, constant.TaskActionRemix,
	})
	var total int64
	if err := query.Count(&total).Error; err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to load video history")
		return
	}
	var tasks []*model.Task
	if err := query.Omit("channel_id", "data").Order("id DESC").Offset(page.GetStartIdx()).Limit(page.PageSize).Find(&tasks).Error; err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to load video history")
		return
	}
	page.SetTotal(int(total))
	page.SetItems(tasksToDto(tasks, false, common.RoleCommonUser))
	common.ApiSuccess(c, page)
}

func GetVideoWorkspaceArtifacts(c *gin.Context) {
	// Deliberately do not use the dashboard admin bypass: even administrators
	// access only their own task results through the customer workspace.
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		writeTaskArtifactError(c, http.StatusInternalServerError, "artifact_internal_error", "Failed to query task")
		return
	}
	if !exists || task == nil || !task.ResultRetrievable() {
		writeTaskArtifactError(c, http.StatusNotFound, "artifact_not_found", "Task or artifact not found")
		return
	}
	artifacts, err := projectTaskArtifacts(task)
	if err != nil {
		writeTaskArtifactProjectionError(c, err)
		return
	}
	items := make([]taskArtifactResponse, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Type != "video" {
			continue
		}
		items = append(items, taskArtifactResponse{Key: artifact.Key, Type: artifact.Type, MimeType: artifact.MimeType,
			ContentURL: "/api/video-workspace/tasks/" + url.PathEscape(task.TaskID) + "/artifacts/" + url.PathEscape(artifact.Key) + "/content"})
	}
	response := gin.H{"task_id": task.TaskID, "artifacts": items}
	if legacyVideoAvailable(task) {
		response["legacy_content_url"] = "/api/video-workspace/tasks/" + url.PathEscape(task.TaskID) + "/artifacts/video/content"
	}
	common.ApiSuccess(c, response)
}

func VideoWorkspaceArtifactContent(c *gin.Context) {
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		writeTaskArtifactError(c, http.StatusInternalServerError, "artifact_internal_error", "Failed to query task")
		return
	}
	if !exists || task == nil || !task.ResultRetrievable() {
		writeTaskArtifactError(c, http.StatusNotFound, "artifact_not_found", "Task or artifact not found")
		return
	}
	c.Header("Cross-Origin-Resource-Policy", "same-origin")
	// The existing content controller has a dashboard-admin branch. Limit this
	// customer endpoint to the owner already verified above.
	c.Set("role", common.RoleCommonUser)
	c.Params = append(c.Params, gin.Param{Key: "key", Value: task.TaskID})
	TaskArtifactContent(c)
}
