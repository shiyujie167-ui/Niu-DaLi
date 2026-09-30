package controller

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"slices"
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
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

const videoWorkspaceMaxPromptLength = 4000

var videoWorkspaceSubmissions sync.Map
var videoWorkspaceClientID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$`)

type videoWorkspaceModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	SupportsImage       bool     `json:"supports_image"`
	SupportsVideo       bool     `json:"supports_video"`
	MaxReferenceImages  int      `json:"max_reference_images"`
	MaxReferenceVideos  int      `json:"max_reference_videos"`
	MaxOutputs          int      `json:"max_outputs"`
	SupportsMixedMedia  bool     `json:"supports_mixed_media"`
	Durations           []int    `json:"durations,omitempty"`
	Sizes               []string `json:"sizes,omitempty"`
	Resolutions         []string `json:"resolutions,omitempty"`
	DefaultResolution   string   `json:"default_resolution,omitempty"`
	MaxImageBytes       int64    `json:"max_image_bytes,omitempty"`
	MaxPromptLength     int      `json:"max_prompt_length"`
	SupportedImageTypes []string `json:"supported_image_types,omitempty"`
	resolutionMetadata  bool
	imageUploadField    string
	Group               string `json:"-"`
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
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	groups := make([]string, 0)
	for group := range service.GetUserUsableGroups(userGroup) {
		if group != "" && group != "auto" && ratio_setting.ContainsGroupRatio(group) {
			groups = append(groups, group)
		}
	}
	if len(groups) == 0 {
		return items, nil
	}
	var abilities []model.Ability
	if err := model.DB.Where(map[string]any{"group": groups, "enabled": true}).Find(&abilities).Error; err != nil {
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
	type modelGroup struct{ model, group string }
	seen := make(map[modelGroup]bool)
	blocked := make(map[modelGroup]bool)
	for _, ability := range abilities {
		route := modelGroup{ability.Model, ability.Group}
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
				blocked[route] = true
				continue
			}
		}
		if channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != "" && strings.TrimSpace(*channel.ParamOverride) != "{}" {
			blocked[route] = true
			continue
		}
		if seen[route] {
			continue
		}
		item := videoWorkspaceModel{ID: ability.Model, Name: ability.Model, Group: ability.Group, MaxPromptLength: videoWorkspaceMaxPromptLength, MaxOutputs: 1, imageUploadField: "input_reference"}
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
			item.MaxReferenceImages, item.imageUploadField = 9, "reference_images"
			item.DefaultResolution = "720p"
			switch item.ID {
			case "Sd-2.0满血933":
				item.Resolutions = []string{"720p", "1080p", "4k"}
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 5000)
			case "Sd-2.0fast":
				item.Resolutions = []string{"720p", "1080p"}
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 5000)
			case "Sd-2.5":
				item.MaxReferenceImages = 30
				item.Resolutions = []string{"480p", "720p", "1080p"}
				maxDuration = 30
				item.MaxPromptLength = min(videoWorkspaceMaxPromptLength, 15000)
			case "Sd-2.0mini":
				item.SupportsVideo, item.MaxReferenceVideos = true, 3
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
			item.MaxReferenceImages = max(1, item.MaxReferenceImages)
			item.SupportedImageTypes = []string{"image/png", "image/jpeg", "image/webp"}
		}
		items = append(items, item)
		seen[route] = true
	}
	items = slices.DeleteFunc(items, func(item videoWorkspaceModel) bool { return blocked[modelGroup{item.ID, item.Group}] })
	// Keep one route per model: prefer the account's own group, then use a
	// stable group order. Rebuild this catalog on submission so permissions,
	// channel availability and the billing group are checked together.
	slices.SortFunc(items, func(a, b videoWorkspaceModel) int {
		if order := cmp.Compare(a.ID, b.ID); order != 0 {
			return order
		}
		if (a.Group == userGroup) != (b.Group == userGroup) {
			if a.Group == userGroup {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Group, b.Group)
	})
	items = slices.CompactFunc(items, func(a, b videoWorkspaceModel) bool { return a.ID == b.ID })
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

type videoWorkspaceVideoReference struct {
	TaskID      string `json:"task_id"`
	ArtifactKey string `json:"artifact_key"`
}

// Only the owner's completed, credentialless artifact can be handed to another
// provider. Dashboard URLs and authenticated provider content endpoints are not
// usable media references and must never be forwarded with channel credentials.
func videoWorkspaceReferenceURL(userID int, reference videoWorkspaceVideoReference) (string, error) {
	task, exists, err := model.GetByTaskId(userID, reference.TaskID)
	if err != nil || !exists || task == nil || task.Status != model.TaskStatusSuccess || !task.ResultRetrievable() {
		return "", errors.New("Reference video must be one of your completed tasks")
	}
	rawURL := ""
	if legacyVideoAvailable(task) && reference.ArtifactKey == "video" {
		rawURL = task.GetResultURL()
	} else {
		artifacts, err := projectTaskArtifacts(task)
		if err != nil || !slices.ContainsFunc(artifacts, func(artifact relaychannel.TaskArtifact) bool {
			return artifact.Key == reference.ArtifactKey && artifact.Type == "video"
		}) {
			return "", errors.New("Reference video artifact is unavailable")
		}
		adaptor, err := initTaskArtifactAdaptor(task)
		if err != nil {
			return "", errors.New("Reference video provider is unavailable")
		}
		provider, ok := adaptor.(relaychannel.TaskContentRequestProvider)
		if !ok {
			return "", errors.New("Reference video cannot be reused by this provider")
		}
		content, err := provider.BuildContentRequest(task, reference.ArtifactKey, relaychannel.TaskArtifactClientRequest{Method: http.MethodGet})
		if err != nil || content == nil || !content.Credentialless || len(content.Headers) != 0 || len(content.Body) != 0 || content.Method != "" && content.Method != http.MethodGet {
			return "", errors.New("Reference video requires provider authentication and cannot be reused")
		}
		rawURL = content.URL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || len(rawURL) > 8192 || parsed.User != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" || strings.ContainsAny(rawURL, "\\\r\n\t ") {
		return "", errors.New("Reference video has no reusable media URL")
	}
	return rawURL, nil
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
	var imageAssetIDs []string
	var videoReferences []videoWorkspaceVideoReference
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
				if key == "seconds" || key == "n" {
					fields[key] = strconv.FormatFloat(value, 'f', -1, 64)
					continue
				}
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid video parameter type"}})
				return
			case []any:
				if key != "image_asset_ids" && key != "video_references" || len(value) > 30 {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid video media references"}})
					return
				}
				encoded, err := common.Marshal(value)
				if err == nil && key == "image_asset_ids" {
					err = common.Unmarshal(encoded, &imageAssetIDs)
				} else if err == nil {
					err = common.Unmarshal(encoded, &videoReferences)
					for _, entry := range value {
						object, ok := entry.(map[string]any)
						if !ok || len(object) != 2 || object["task_id"] == nil || object["artifact_key"] == nil {
							err = errors.New("invalid video reference")
						}
					}
				}
				if err != nil {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid video media references"}})
					return
				}
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
		if !slices.Contains([]string{"model", "prompt", "seconds", "size", "resolution", "n", "canvas_node_id", "submission_id"}, key) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Unsupported video parameter: " + key}})
			return
		}
	}
	if raw, exists := fields["n"]; exists && raw != "1" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "The selected video models generate exactly one output per task"}})
		return
	}
	nodeID, submissionID := fields["canvas_node_id"], fields["submission_id"]
	if nodeID != "" || submissionID != "" {
		if !videoWorkspaceClientID.MatchString(nodeID) || !videoWorkspaceClientID.MatchString(submissionID) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "A valid canvas node and submission ID are required together"}})
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
	imageCount := len(imageAssetIDs)
	if upload != nil {
		imageCount++
	}
	if imageCount > selected.MaxReferenceImages || len(videoReferences) > selected.MaxReferenceVideos {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "The selected model does not support this number of reference images or videos"}})
		return
	}
	if imageCount > 0 && len(videoReferences) > 0 && !selected.SupportsMixedMedia {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "The selected model cannot combine uploaded images and reference videos"}})
		return
	}
	type referenceImage struct {
		data     []byte
		mimeType string
	}
	images := make([]referenceImage, 0, imageCount)
	if upload != nil {
		if !selected.SupportsImage || upload.Size <= 0 || upload.Size > selected.MaxImageBytes {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Reference image is unsupported or exceeds the model upload limit"}})
			return
		}
		var imageBytes []byte
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
		imageType := "image/" + format
		if err != nil || !slices.Contains(selected.SupportedImageTypes, imageType) || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Upload a valid PNG, JPEG, or WebP image up to 40 megapixels"}})
			return
		}
		images = append(images, referenceImage{imageBytes, imageType})
	}
	var totalImageBytes int64
	seenImages := make(map[string]bool, len(imageAssetIDs))
	for _, id := range imageAssetIDs {
		if !videoWorkspaceClientID.MatchString(id) || seenImages[id] {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Each reference image must be a distinct saved asset"}})
			return
		}
		seenImages[id] = true
		asset, err := model.GetVideoWorkspaceAsset(userID, id)
		if err != nil || asset == nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Reference image is not one of your saved assets"}})
			return
		}
		if asset.Size <= 0 || asset.Size > selected.MaxImageBytes || !slices.Contains(selected.SupportedImageTypes, asset.MimeType) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Reference image is unsupported or exceeds the model upload limit"}})
			return
		}
		totalImageBytes += asset.Size
		if totalImageBytes > 64<<20 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Combined reference images must not exceed 64 MiB"}})
			return
		}
		data, err := asset.Bytes()
		if err != nil || int64(len(data)) != asset.Size {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "Failed to read saved reference image"}})
			return
		}
		images = append(images, referenceImage{data, asset.MimeType})
	}
	videoURLs := make([]string, 0, len(videoReferences))
	seenVideos := make(map[videoWorkspaceVideoReference]bool, len(videoReferences))
	for _, reference := range videoReferences {
		if !videoWorkspaceClientID.MatchString(reference.TaskID) || !taskArtifactKeyPattern.MatchString(reference.ArtifactKey) || seenVideos[reference] {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Each reference video must identify a distinct completed task artifact"}})
			return
		}
		seenVideos[reference] = true
		mediaURL, err := videoWorkspaceReferenceURL(userID, reference)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		videoURLs = append(videoURLs, mediaURL)
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
	if len(videoURLs) > 0 {
		encoded, err := common.Marshal(videoURLs)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		_ = writer.WriteField("video_urls", string(encoded))
	}
	for _, image := range images {
		headers := make(textproto.MIMEHeader)
		headers.Set("Content-Disposition", `form-data; name="`+selected.imageUploadField+`"; filename="reference.`+strings.TrimPrefix(image.mimeType, "image/")+`"`)
		headers.Set("Content-Type", image.mimeType)
		part, err := writer.CreatePart(headers)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		_, _ = part.Write(image.data)
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
	group := selected.Group
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
	if submissionID != "" {
		run, created, err := model.ClaimVideoWorkspaceSubmission(userID, nodeID, submissionID)
		if errors.Is(err, model.ErrVideoCanvasSubmissionInvalid) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Save a valid generation node on your canvas before submitting"}})
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "Failed to reserve canvas submission"}})
			return
		}
		if !created {
			if run.NodeID == nodeID && run.TaskID != "" {
				c.AbortWithStatusJSON(http.StatusOK, gin.H{"id": run.TaskID, "object": "video"})
				return
			}
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": gin.H{"message": "This canvas submission has already been attempted. Refresh the canvas to recover its task before generating again."}, "submission_id": submissionID, "submission_status": run.Status})
			return
		}
		c.Set("video_workspace_canvas_node_id", nodeID)
		c.Set("video_workspace_submission_id", submissionID)
		// A disconnect or provider-side timeout can leave the upstream outcome
		// ambiguous. Keep the durable claim until the existing task is recovered;
		// replaying the same request must never create another billable task.
		defer func() {
			if err := model.FailVideoWorkspaceSubmission(userID, submissionID, c.GetBool("video_workspace_upstream_attempted")); err != nil {
				common.SysError("Failed to record canvas submission outcome: " + err.Error())
			}
		}()
	}
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
