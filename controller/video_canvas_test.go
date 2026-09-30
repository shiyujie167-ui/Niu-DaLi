package controller

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestVideoCanvasOwnerEndpoints(t *testing.T) {
	db, engine, calls := videoWorkspaceTestRouter(t, common.RoleRootUser)
	require.NoError(t, db.AutoMigrate(&model.VideoWorkspaceCanvas{}, &model.VideoWorkspaceAsset{}, &model.VideoWorkspaceSubmission{}))
	engine.GET("/api/video-workspace/canvas", GetVideoWorkspaceCanvas)
	engine.PUT("/api/video-workspace/canvas", middleware.BrowserOriginGuard(), PutVideoWorkspaceCanvas)
	engine.POST("/api/video-workspace/assets", middleware.BrowserOriginGuard(), UploadVideoWorkspaceAsset)
	engine.GET("/api/video-workspace/assets/:asset_id/content", VideoWorkspaceAssetContent)
	engine.GET("/api/video-workspace/tasks/:task_id", GetVideoWorkspaceTask)
	for _, tc := range []struct {
		name, origin, body string
		status             int
	}{
		{"foreign origin", "https://foreign.example", `{"revision":0,"graph":` + canvasTestGraph + `}`, http.StatusForbidden},
		{"save own graph", "http://workspace.example", `{"revision":0,"graph":` + canvasTestGraph + `}`, http.StatusOK},
		{"stale revision", "http://workspace.example", `{"revision":0,"graph":` + canvasTestGraph + `}`, http.StatusConflict},
		{"invalid version", "http://workspace.example", `{"revision":1,"graph":{"schema_version":2,"nodes":[],"edges":[],"viewport":{"zoom":1}}}`, http.StatusBadRequest},
		{"embedded media", "http://workspace.example", `{"revision":1,"graph":{"schema_version":1,"nodes":[{"id":"image","type":"image","position":{"x":0,"y":0},"data":{"base64":"bytes"}}],"edges":[],"viewport":{"zoom":1}}}`, http.StatusBadRequest},
		{"dangling connection", "http://workspace.example", `{"revision":1,"graph":{"schema_version":1,"nodes":[],"edges":[{"id":"edge","source":"missing","target":"missing"}],"viewport":{"zoom":1}}}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "http://workspace.example/api/video-workspace/canvas", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", tc.origin)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, tc.status, response.Code, response.Body.String())
		})
	}
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewNRGBA(image.Rect(0, 0, 1, 1))))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "reference.png")
	require.NoError(t, err)
	_, err = part.Write(content.Bytes())
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "http://workspace.example/api/video-workspace/assets", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "http://workspace.example")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var upload struct {
		Data struct {
			ID         string
			ContentURL string `json:"content_url"`
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &upload))
	require.NotEmpty(t, upload.Data.ID)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, upload.Data.ContentURL, nil))
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, content.Bytes(), response.Body.Bytes())
	assert.Equal(t, "image/png", response.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	foreign, err := model.CreateVideoWorkspaceAsset(&model.VideoWorkspaceAsset{UserID: 8, Filename: "private.png", MimeType: "image/png", Data: content.Bytes()})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Task{UserId: 8, TaskID: "foreign-task", Action: constant.TaskActionTextToVideo, Status: model.TaskStatusSuccess}).Error)
	for _, path := range []string{"/api/video-workspace/assets/" + foreign.ID + "/content", "/api/video-workspace/tasks/foreign-task"} {
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, response.Code, "root workspace access is still owner-only")
	}
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/canvas", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var document struct {
		Data struct {
			Revision int64
			Graph    common.RawMessage
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &document))
	assert.EqualValues(t, 1, document.Data.Revision)
	assert.JSONEq(t, canvasTestGraph, string(document.Data.Graph))
	assert.Zero(t, calls.Load(), "editing, uploading and reading never call generation")
}

const canvasTestGraph = `{"schema_version":1,"nodes":[{"id":"prompt","type":"prompt","position":{"x":10,"y":20},"data":{"prompt":"Sunrise"}},{"id":"generate","type":"generation","position":{"x":410,"y":20},"data":{"model":"sora-2","count":1}}],"edges":[{"id":"link","source":"prompt","target":"generate"}],"viewport":{"x":-100,"y":10,"zoom":0.75}}`

func videoCanvasDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "canvas.db") + "?_pragma=busy_timeout(30000)&_txlock=immediate")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: "canvas_regression_"}})
	require.NoError(t, err)
	previous := model.DB
	previousType := common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseType(dialect))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&model.VideoWorkspaceSubmission{}, &model.VideoWorkspaceAsset{}, &model.VideoWorkspaceCanvas{}, &model.Task{}))
		require.NoError(t, sqlDB.Close())
		model.DB = previous
		common.SetMainDatabaseType(previousType)
	})
	var version string
	query := "SELECT VERSION()"
	if dialect == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("%s engine: %s", dialect, version)
	return db
}

func TestVideoCanvasDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := videoCanvasDatabase(t, dialect)
			// Existing task columns do not change; old properties must remain readable.
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			legacy := &model.Task{TaskID: "legacy", UserId: 1, Status: model.TaskStatusSuccess, Action: constant.TaskActionTextToVideo, Properties: model.Properties{Input: "Existing saved prompt"}}
			require.NoError(t, db.Create(legacy).Error)
			tables := []any{&model.VideoWorkspaceCanvas{}, &model.VideoWorkspaceAsset{}, &model.VideoWorkspaceSubmission{}}
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			var existing model.Task
			require.NoError(t, db.First(&existing, legacy.ID).Error)
			assert.Equal(t, "Existing saved prompt", existing.Properties.Input)
			assert.Empty(t, existing.Properties.CanvasSubmissionID)
			graph, err := validateVideoCanvasGraph(1, []byte(canvasTestGraph))
			require.NoError(t, err)
			saved, err := model.SaveVideoWorkspaceCanvas(1, 0, graph)
			require.NoError(t, err)
			assert.EqualValues(t, 1, saved.Revision)
			_, err = model.SaveVideoWorkspaceCanvas(1, 0, graph)
			require.ErrorIs(t, err, model.ErrVideoCanvasConflict)
			require.NoError(t, db.AutoMigrate(tables...))
			restored, err := model.GetVideoWorkspaceCanvas(1)
			require.NoError(t, err)
			assert.JSONEq(t, canvasTestGraph, string(restored.Graph))
			other, err := model.GetVideoWorkspaceCanvas(2)
			require.NoError(t, err)
			assert.Zero(t, other.Revision)
			assert.NotEqual(t, restored.Graph, other.Graph)
			// Binary storage must exceed the 64 KiB MySQL BLOB boundary without truncation.
			payload := bytes.Repeat([]byte{137, 80, 78, 71}, 20000)
			asset, err := model.CreateVideoWorkspaceAsset(&model.VideoWorkspaceAsset{UserID: 1, Filename: "reference.png", MimeType: "image/png", Width: 1, Height: 1, Data: payload})
			require.NoError(t, err)
			duplicate, err := model.CreateVideoWorkspaceAsset(&model.VideoWorkspaceAsset{UserID: 1, Filename: "copy.png", MimeType: "image/png", Width: 1, Height: 1, Data: payload})
			require.NoError(t, err)
			assert.Equal(t, asset.ID, duplicate.ID)
			require.NoError(t, db.AutoMigrate(tables...))
			loaded, err := model.GetVideoWorkspaceAsset(1, asset.ID)
			require.NoError(t, err)
			assert.Equal(t, payload, loaded.Data)
			_, err = model.GetVideoWorkspaceAsset(2, asset.ID)
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			restored, err = model.GetVideoWorkspaceCanvas(1)
			require.NoError(t, err)
			assert.EqualValues(t, len(payload), restored.AssetBytes)
			assert.Error(t, db.Create(&model.VideoWorkspaceCanvas{UserID: 1, Graph: graph}).Error)
			run, created, err := model.ClaimVideoWorkspaceSubmission(1, "generate", "run-1")
			require.NoError(t, err)
			require.True(t, created)
			assert.Equal(t, model.VideoWorkspaceSubmissionSubmitting, run.Status)
			pending, created, err := model.ClaimVideoWorkspaceSubmission(1, "generate", "run-2")
			require.NoError(t, err)
			assert.False(t, created)
			assert.Equal(t, "run-1", pending.SubmissionID)
			task := &model.Task{TaskID: "canvas-task", UserId: 1, Status: model.TaskStatusInProgress, Action: constant.TaskActionTextToVideo, Properties: model.Properties{CanvasNodeID: "generate", CanvasSubmissionID: "run-1"}}
			require.NoError(t, task.InsertWithContext(context.Background()))
			run, created, err = model.ClaimVideoWorkspaceSubmission(1, "generate", "run-1")
			require.NoError(t, err)
			assert.False(t, created)
			assert.Equal(t, "canvas-task", run.TaskID)
			assert.Equal(t, model.VideoWorkspaceSubmissionAccepted, run.Status)
			pending, created, err = model.ClaimVideoWorkspaceSubmission(1, "generate", "run-2")
			require.NoError(t, err)
			assert.False(t, created)
			assert.Equal(t, "canvas-task", pending.TaskID)
			invalid := &model.Task{TaskID: "must-rollback", UserId: 2, Properties: model.Properties{CanvasSubmissionID: "run-1"}}
			require.Error(t, invalid.InsertWithContext(context.Background()))
			var count int64
			require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", "must-rollback").Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.Model(task).Update("status", model.TaskStatusSuccess).Error)
			_, created, err = model.ClaimVideoWorkspaceSubmission(1, "generate", "run-2")
			require.NoError(t, err)
			require.True(t, created)
			require.NoError(t, model.FailVideoWorkspaceSubmission(1, "run-2", false))
			_, created, err = model.ClaimVideoWorkspaceSubmission(1, "generate", "run-3")
			require.NoError(t, err)
			require.True(t, created)
			require.NoError(t, model.FailVideoWorkspaceSubmission(1, "run-3", true))
			pending, created, err = model.ClaimVideoWorkspaceSubmission(1, "generate", "run-4")
			require.NoError(t, err)
			assert.False(t, created)
			assert.Equal(t, model.VideoWorkspaceSubmissionUnknown, pending.Status)
			require.NoError(t, db.AutoMigrate(tables...))
			restored, err = model.GetVideoWorkspaceCanvas(1)
			require.NoError(t, err)
			assert.JSONEq(t, canvasTestGraph, string(restored.Graph))
		})
	}
}
