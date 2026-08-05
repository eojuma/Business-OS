package customers

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)

	group := rg.Group("/customers")
	{
		group.POST("", handler.Create)
		group.GET("", handler.List)
		group.GET("/above-balance", handler.ListAboveBalance)
		group.GET("/:id", handler.Get)
		group.PATCH("/:id", handler.Update)
	}
}