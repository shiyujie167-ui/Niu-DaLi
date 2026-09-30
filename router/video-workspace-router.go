package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetVideoWorkspaceRouter(router *gin.Engine) {
	workspace := router.Group("/api/video-workspace")
	workspace.Use(middleware.RouteTag("api"), middleware.BodyStorageCleanup(), middleware.UserAuth(), controller.VideoWorkspaceSession)
	workspace.GET("/models", controller.GetVideoWorkspaceModels)
	workspace.GET("/canvas", controller.GetVideoWorkspaceCanvas)
	workspace.PUT("/canvas", middleware.BrowserOriginGuard(), controller.PutVideoWorkspaceCanvas)
	workspace.POST("/assets", middleware.BrowserOriginGuard(), middleware.UserCriticalRateLimit("video-workspace-assets"), controller.UploadVideoWorkspaceAsset)
	workspace.GET("/assets/:asset_id/content", controller.VideoWorkspaceAssetContent)
	workspace.HEAD("/assets/:asset_id/content", controller.VideoWorkspaceAssetContent)
	workspace.GET("/tasks", controller.GetVideoWorkspaceTasks)
	workspace.GET("/tasks/:task_id", controller.GetVideoWorkspaceTask)
	workspace.GET("/tasks/:task_id/artifacts", controller.GetVideoWorkspaceArtifacts)
	workspace.GET("/tasks/:task_id/artifacts/:artifact_key/content", controller.VideoWorkspaceArtifactContent)
	workspace.HEAD("/tasks/:task_id/artifacts/:artifact_key/content", controller.VideoWorkspaceArtifactContent)
	workspace.POST("/tasks", middleware.BrowserOriginGuard(), middleware.UserCriticalRateLimit("video-workspace"), middleware.SystemPerformanceCheck(),
		controller.PrepareVideoWorkspaceSubmission,
		middleware.PinTaskPluginEndpoint(), middleware.ModelRequestRateLimit(), middleware.PrepareTaskPluginEndpoint(), middleware.Distribute(),
		func(c *gin.Context) { controller.RelayTaskPluginEndpoint(c, controller.RelayTask) },
	)
}
