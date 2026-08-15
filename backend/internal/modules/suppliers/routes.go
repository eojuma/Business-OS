package suppliers

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	handler := NewHandler(NewService(NewRepository(db)))
	group := rg.Group("/suppliers")
	group.POST("", handler.Create)
	group.GET("", handler.List)
	group.GET("/:id", handler.Get)
	group.PATCH("/:id", handler.Update)
	group.DELETE("/:id", handler.Delete)
	group.POST("/:id/payments", handler.RecordPayment)
	group.GET("/:id/payments", handler.ListPayments)
}
