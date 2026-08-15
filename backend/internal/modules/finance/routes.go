package finance

import (
	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	h := NewHandler(NewService(NewRepository(db)))
	group := rg.Group("/finance")
	group.POST("/expenses", h.CreateExpense)
	group.GET("/expenses", h.ListExpenses)
	group.DELETE("/expenses/:id", h.DeleteExpense)
	group.GET("/summary", h.Summary)
}
