package assistant

import (
	"net/http"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	group := rg.Group("/assistant")
	{
		group.GET("", func(c *gin.Context) {
			response.Success(c, http.StatusOK, gin.H{
				"module":  "assistant",
				"message": "Assistant module scaffolded — implement model/repository/service/handler",
			})
		})
	}
}
