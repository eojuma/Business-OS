package invoices

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterRoutes mounts the invoice endpoints.
// Authenticated: GET /invoices, GET /invoices/:id, POST /invoices/:id/payments.
func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	handler := NewHandler(NewService(NewRepository(db)))
	group := rg.Group("/invoices")
	{
		group.GET("", handler.List)
		group.GET("/:id", handler.Get)
		group.POST("/:id/payments", handler.RecordPayment)
	}
}
