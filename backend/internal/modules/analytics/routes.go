package analytics

import (
	"net/http"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	group := rg.Group("/analytics")
	{
		group.GET("", func(c *gin.Context) {
			response.Success(c, http.StatusOK, gin.H{
				"module":  "analytics",
				"message": "Analytics module scaffolded — implement model/repository/service/handler",
			})
		})
	}
}
