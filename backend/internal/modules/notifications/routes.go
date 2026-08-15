package notifications

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	h := NewHandler(NewService(NewRepository(db)))
	rg.GET("/notifications", h.List)
}
