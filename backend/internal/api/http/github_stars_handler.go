package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
)

type githubStarsProvider interface {
	Get(context.Context) (app.GitHubStars, error)
}

type GitHubStarsHandler struct {
	provider githubStarsProvider
}

func NewGitHubStarsHandler(provider githubStarsProvider) *GitHubStarsHandler {
	return &GitHubStarsHandler{provider: provider}
}

func (h *GitHubStarsHandler) Get(c *gin.Context) {
	result, err := h.provider.Get(c.Request.Context())
	if err != nil {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "暂时无法获取 GitHub Star 数，请稍后再试",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stars": result.Count,
		"stale": result.Stale,
	})
}
