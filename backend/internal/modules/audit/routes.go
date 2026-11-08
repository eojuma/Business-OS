package audit

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterRoutes mounts the read-only audit viewer endpoint.
// Authenticated: GET /audit.
func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	handler := NewHandler(NewService(NewRepository(db)))
	rg.GET("/audit", handler.List)
}
