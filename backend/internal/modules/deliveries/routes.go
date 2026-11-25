package deliveries

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/notifications"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	notifRepo := notifications.NewRepository(db)
	svc := NewService(repo, notifRepo)
	handler := NewHandler(svc)

	group := rg.Group("/deliveries")
	{
		group.POST("", handler.Create)
		group.GET("", handler.List)
		group.GET("/:id", handler.Get)
		group.DELETE("/:id", handler.Delete)
		group.POST("/:id/transition", handler.Transition)
		group.GET("/:id/history", handler.History)
		group.GET("/:id/note", handler.Note)
	}
}
