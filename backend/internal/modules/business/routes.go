package business

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)

	group := rg.Group("/business")
	{
		group.POST("", handler.Create)

		protected := group.Group("")
		protected.Use(middleware.RequireAuth(cfg))
		{
			protected.GET("", handler.Get)
			protected.PATCH("", handler.Update)
		}
	}
}