package controller

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const videoCanvasMaxBytes = 1 << 20

type videoCanvasPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type videoCanvasViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

type videoCanvasNode struct {
	ID       string                       `json:"id"`
	Type     string                       `json:"type"`
	Position *videoCanvasPoint            `json:"position"`
	Data     map[string]common.RawMessage `json:"data"`
}

type videoCanvasEdge struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	SourceHandle *string `json:"sourceHandle,omitempty"`
	TargetHandle *string `json:"targetHandle,omitempty"`
}

type videoCanvasGraph struct {
	SchemaVersion int                  `json:"schema_version"`
	Nodes         []videoCanvasNode    `json:"nodes"`
	Edges         []videoCanvasEdge    `json:"edges"`
	Viewport      *videoCanvasViewport `json:"viewport"`
}

// Every object has an explicit contract. React Flow's transient selection and
// measured dimensions are intentionally excluded from the persisted document.
func decodeVideoCanvasObject(raw []byte, output any, keys ...string) error {
	var fields map[string]common.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("expected a JSON object")
	}
	for key := range fields {
		if !slices.Contains(keys, key) {
			return fmt.Errorf("unsupported canvas field: %s", key)
		}
	}
	return common.Unmarshal(raw, output)
}

func validVideoCanvasID(id string) bool {
	if id == "" || len(id) > 96 {
		return false
	}
	for _, char := range id {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func validateVideoCanvasGraph(userID int, raw []byte) ([]byte, error) {
	var fields struct {
		SchemaVersion int                 `json:"schema_version"`
		Nodes         []common.RawMessage `json:"nodes"`
		Edges         []common.RawMessage `json:"edges"`
		Viewport      common.RawMessage   `json:"viewport"`
	}
	if err := decodeVideoCanvasObject(raw, &fields, "schema_version", "nodes", "edges", "viewport"); err != nil {
		return nil, err
	}
	if fields.SchemaVersion != 1 || fields.Nodes == nil || fields.Edges == nil || len(fields.Nodes) > 200 || len(fields.Edges) > 400 {
		return nil, errors.New("invalid canvas version or graph size")
	}
	graph := videoCanvasGraph{SchemaVersion: 1, Nodes: make([]videoCanvasNode, 0, len(fields.Nodes)), Edges: make([]videoCanvasEdge, 0, len(fields.Edges)), Viewport: &videoCanvasViewport{}}
	if err := decodeVideoCanvasObject(fields.Viewport, graph.Viewport, "x", "y", "zoom"); err != nil {
		return nil, err
	}
	if math.Abs(graph.Viewport.X) > 1e7 || math.Abs(graph.Viewport.Y) > 1e7 || graph.Viewport.Zoom < 0.05 || graph.Viewport.Zoom > 10 {
		return nil, errors.New("invalid canvas viewport")
	}
	nodes := make(map[string]string, len(fields.Nodes))
	for _, rawNode := range fields.Nodes {
		var node videoCanvasNode
		if err := decodeVideoCanvasObject(rawNode, &node, "id", "type", "position", "data"); err != nil {
			return nil, err
		}
		if !validVideoCanvasID(node.ID) || nodes[node.ID] != "" || node.Position == nil || math.Abs(node.Position.X) > 1e7 || math.Abs(node.Position.Y) > 1e7 || node.Data == nil {
			return nil, errors.New("invalid or duplicate canvas node")
		}
		var rawPosition struct {
			Position common.RawMessage `json:"position"`
		}
		_ = common.Unmarshal(rawNode, &rawPosition)
		if err := decodeVideoCanvasObject(rawPosition.Position, node.Position, "x", "y"); err != nil {
			return nil, err
		}
		var allowed []string
		switch node.Type {
		case "prompt":
			allowed = []string{"prompt"}
		case "image":
			allowed = []string{"asset_id", "filename", "mime_type", "size", "width", "height", "content_url"}
		case "generation":
			allowed = []string{"model", "prompt", "seconds", "size", "resolution", "count", "task_id", "artifact_key"}
		default:
			return nil, errors.New("unsupported canvas node type")
		}
		for key, value := range node.Data {
			if !slices.Contains(allowed, key) {
				return nil, fmt.Errorf("unsupported canvas node data: %s", key)
			}
			if key == "count" || node.Type == "image" && slices.Contains([]string{"size", "width", "height"}, key) {
				var number int64
				if err := common.Unmarshal(value, &number); err != nil || number < 0 || number > model.VideoWorkspaceMaxAssetBytes {
					return nil, errors.New("invalid canvas numeric field")
				}
				continue
			}
			var text string
			if err := common.Unmarshal(value, &text); err != nil || string(value) == "null" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > videoWorkspaceMaxPromptLength {
				return nil, errors.New("invalid canvas text field")
			}
			if key != "prompt" && len(text) > 512 {
				return nil, errors.New("canvas field is too long")
			}
		}
		if node.Type == "image" {
			var assetID string
			_ = common.Unmarshal(node.Data["asset_id"], &assetID)
			if assetID != "" {
				var asset model.VideoWorkspaceAsset
				if err := model.DB.Omit("data").Where("user_id = ? AND id = ?", userID, assetID).First(&asset).Error; err != nil {
					return nil, errors.New("canvas image is unavailable")
				}
				for key, value := range map[string]any{"asset_id": asset.ID, "filename": asset.Filename, "mime_type": asset.MimeType, "size": asset.Size, "width": asset.Width, "height": asset.Height, "content_url": "/api/video-workspace/assets/" + asset.ID + "/content"} {
					node.Data[key], _ = common.Marshal(value)
				}
			} else {
				node.Data = map[string]common.RawMessage{"asset_id": common.RawMessage(`""`)}
			}
		}
		if node.Type == "generation" {
			var taskID string
			_ = common.Unmarshal(node.Data["task_id"], &taskID)
			if taskID != "" {
				task, exists, err := model.GetByTaskId(userID, taskID)
				if err != nil || !exists || task == nil || !slices.Contains([]string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo, constant.TaskActionFirstTailToVideo, constant.TaskActionReferenceToVideo, constant.TaskActionRemix}, task.Action) {
					return nil, errors.New("canvas task is unavailable")
				}
			}
		}
		nodes[node.ID] = node.Type
		graph.Nodes = append(graph.Nodes, node)
	}
	edgeIDs := make(map[string]bool, len(fields.Edges))
	links := make(map[string][]string)
	indegree := make(map[string]int, len(nodes))
	for _, rawEdge := range fields.Edges {
		var edge videoCanvasEdge
		if err := decodeVideoCanvasObject(rawEdge, &edge, "id", "source", "target", "sourceHandle", "targetHandle"); err != nil {
			return nil, err
		}
		if !validVideoCanvasID(edge.ID) || edgeIDs[edge.ID] || nodes[edge.Source] == "" || nodes[edge.Target] != "generation" || edge.Source == edge.Target {
			return nil, errors.New("invalid canvas connection")
		}
		if edge.SourceHandle != nil && len(*edge.SourceHandle) > 96 || edge.TargetHandle != nil && len(*edge.TargetHandle) > 96 {
			return nil, errors.New("invalid canvas connection handle")
		}
		edgeIDs[edge.ID] = true
		links[edge.Source] = append(links[edge.Source], edge.Target)
		indegree[edge.Target]++
		graph.Edges = append(graph.Edges, edge)
	}
	queue := make([]string, 0, len(nodes))
	for id := range nodes {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	for index := 0; index < len(queue); index++ {
		for _, target := range links[queue[index]] {
			indegree[target]--
			if indegree[target] == 0 {
				queue = append(queue, target)
			}
		}
	}
	if len(queue) != len(nodes) {
		return nil, errors.New("canvas connections cannot form a cycle")
	}
	return common.Marshal(graph)
}

func GetVideoWorkspaceCanvas(c *gin.Context) {
	canvas, err := model.GetVideoWorkspaceCanvas(c.GetInt("id"))
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "canvas_error", "Failed to load canvas")
		return
	}
	submissions := make([]model.VideoWorkspaceSubmission, 0)
	if err := model.DB.Where("user_id = ?", c.GetInt("id")).Order("id DESC").Limit(500).Find(&submissions).Error; err != nil {
		videoProxyError(c, http.StatusInternalServerError, "canvas_error", "Failed to load canvas tasks")
		return
	}
	common.ApiSuccess(c, gin.H{"revision": canvas.Revision, "graph": common.RawMessage(canvas.Graph), "submissions": submissions})
}

func PutVideoWorkspaceCanvas(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, videoCanvasMaxBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		videoProxyError(c, http.StatusRequestEntityTooLarge, "canvas_too_large", "Canvas exceeds the 1 MiB limit")
		return
	}
	var input struct {
		Revision *int64            `json:"revision"`
		Graph    common.RawMessage `json:"graph"`
	}
	if err := decodeVideoCanvasObject(raw, &input, "revision", "graph"); err != nil || input.Revision == nil || *input.Revision < 0 || *input.Revision >= 1<<53-1 {
		videoProxyError(c, http.StatusBadRequest, "invalid_canvas", "Invalid canvas revision or document")
		return
	}
	graph, err := validateVideoCanvasGraph(c.GetInt("id"), input.Graph)
	if err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_canvas", err.Error())
		return
	}
	canvas, err := model.SaveVideoWorkspaceCanvas(c.GetInt("id"), *input.Revision, graph)
	if errors.Is(err, model.ErrVideoCanvasConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "canvas_revision_conflict", "message": "Canvas changed in another tab. Reload before saving."})
		return
	}
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "canvas_error", "Failed to save canvas")
		return
	}
	common.ApiSuccess(c, gin.H{"revision": canvas.Revision, "graph": common.RawMessage(canvas.Graph)})
}

func UploadVideoWorkspaceAsset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, model.VideoWorkspaceMaxAssetBytes+(64<<10))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_image", "Invalid or oversized reference image")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	files := c.Request.MultipartForm.File["file"]
	if len(c.Request.MultipartForm.Value) != 0 || len(c.Request.MultipartForm.File) != 1 || len(files) != 1 || files[0].Size <= 0 || files[0].Size > model.VideoWorkspaceMaxAssetBytes {
		videoProxyError(c, http.StatusBadRequest, "invalid_image", "Upload one image up to 10 MiB")
		return
	}
	file, err := files[0].Open()
	if err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_image", "Failed to read reference image")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, model.VideoWorkspaceMaxAssetBytes+1))
	if err != nil || len(data) > model.VideoWorkspaceMaxAssetBytes {
		videoProxyError(c, http.StatusBadRequest, "invalid_image", "Failed to read reference image")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !slices.Contains([]string{"png", "jpeg", "webp"}, format) || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		videoProxyError(c, http.StatusBadRequest, "invalid_image", "Upload a valid PNG, JPEG, or WebP image up to 40 megapixels")
		return
	}
	filename := strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return -1
		}
		return char
	}, filepath.Base(files[0].Filename))
	if filename == "" || !utf8.ValidString(filename) || len(filename) > 255 {
		filename = "reference." + format
	}
	asset, err := model.CreateVideoWorkspaceAsset(&model.VideoWorkspaceAsset{UserID: c.GetInt("id"), Filename: filename, MimeType: "image/" + format, Width: config.Width, Height: config.Height, Data: data})
	if errors.Is(err, model.ErrVideoCanvasAssetQuota) {
		videoProxyError(c, http.StatusRequestEntityTooLarge, "canvas_storage_full", "Canvas image storage limit reached")
		return
	}
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "canvas_error", "Failed to store reference image")
		return
	}
	common.ApiSuccess(c, gin.H{"id": asset.ID, "filename": asset.Filename, "mime_type": asset.MimeType, "size": asset.Size, "width": asset.Width, "height": asset.Height, "content_url": "/api/video-workspace/assets/" + asset.ID + "/content"})
}

func VideoWorkspaceAssetContent(c *gin.Context) {
	asset, err := model.GetVideoWorkspaceAsset(c.GetInt("id"), c.Param("asset_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		videoProxyError(c, http.StatusNotFound, "image_not_found", "Reference image not found")
		return
	}
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "canvas_error", "Failed to load reference image")
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cross-Origin-Resource-Policy", "same-origin")
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": asset.Filename}))
	c.Header("Content-Length", strconv.FormatInt(asset.Size, 10))
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", asset.MimeType)
		c.Status(http.StatusOK)
		return
	}
	c.Data(http.StatusOK, asset.MimeType, asset.Data)
}

func GetVideoWorkspaceTask(c *gin.Context) {
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "task_error", "Failed to load video task")
		return
	}
	if !exists || task == nil || !slices.Contains([]string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo, constant.TaskActionFirstTailToVideo, constant.TaskActionReferenceToVideo, constant.TaskActionRemix}, task.Action) {
		videoProxyError(c, http.StatusNotFound, "task_not_found", "Video task not found")
		return
	}
	items := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)
	common.ApiSuccess(c, items[0])
}
