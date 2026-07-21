package httpapi

import "github.com/gin-gonic/gin"

type Router struct {
	compile  *CompileHandler
	task     *TaskHandler
	download *DownloadHandler
	stars    *GitHubStarsHandler
}

func NewRouter(compile *CompileHandler, task *TaskHandler, download *DownloadHandler, stars *GitHubStarsHandler) *Router {
	return &Router{
		compile:  compile,
		task:     task,
		download: download,
		stars:    stars,
	}
}

func (r *Router) RegisterRoutes(engine *gin.Engine) {
	api := engine.Group("/api")
	api.Use(privateResponseHeaders)
	{
		api.GET("/health", r.compile.HealthCheck)
		api.GET("/github/stars", r.stars.Get)
		api.POST("/compile", r.compile.Compile)
		api.GET("/events", r.task.StreamTaskEvents)
		api.GET("/download/:taskId", r.download.DownloadArtifacts)
		api.HEAD("/download/:taskId", r.download.DownloadArtifacts)
		api.GET("/tasks/:taskId", r.task.GetTask)
		api.GET("/tasks/:taskId/report", r.task.GetTaskReport)
		api.GET("/tasks/:taskId/diagnostics", r.task.GetTaskDiagnostics)
		api.GET("/tasks/:taskId/artifacts", r.task.GetTaskArtifacts)
	}

	engine.Static("/assets", "./frontend/dist/assets")
	engine.GET("/", func(c *gin.Context) {
		c.File("./frontend/dist/index.html")
	})
}

func privateResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Next()
}
