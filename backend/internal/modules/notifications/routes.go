package notifications

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	h := NewHandler(NewService(NewRepository(db)))
	group := rg.Group("/notifications")
	{
		group.GET("", h.List)
		group.GET("/unread-count", h.UnreadCount)
		group.POST("/read-all", h.MarkAllRead)
		group.POST("/:id/read", h.MarkRead)
	}
}
