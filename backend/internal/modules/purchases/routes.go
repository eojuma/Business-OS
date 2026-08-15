package purchases

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	handler := NewHandler(NewService(NewRepository(db), inventory.NewRepository(db), suppliers.NewRepository(db), products.NewRepository(db)))
	group := rg.Group("/purchases")
	group.POST("", handler.Create)
	group.GET("", handler.List)
	group.GET("/:id", handler.Get)
	group.POST("/:id/receive", handler.Receive)
}
