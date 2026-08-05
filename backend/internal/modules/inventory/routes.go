package inventory

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)

	group := rg.Group("/inventory")
	{
		group.POST("/movements", handler.RecordMovement)
		group.GET("/movements/:productId", handler.ListMovements)
		group.GET("/levels/:productId", handler.GetStockLevel)
		group.GET("/low-stock", handler.ListLowStock)
	}
}