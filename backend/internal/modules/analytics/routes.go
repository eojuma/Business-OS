package analytics

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	h := NewHandler(NewService(NewRepository(db)))
	g := rg.Group("/analytics")
	g.GET("/overview", h.Overview)
	g.GET("/top-products", h.TopProducts)
	g.GET("/slow-moving", h.SlowMoving)
}
