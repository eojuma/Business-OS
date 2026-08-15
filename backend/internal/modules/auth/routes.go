package auth

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	businessRepo := business.NewRepository(db)
	svc := NewService(repo, cfg, businessRepo)
	handler := NewHandler(svc)

	authGroup := rg.Group("/auth")
	{
		authGroup.POST("/register", handler.Register)
		authGroup.POST("/login", handler.Login)
	}
	team := rg.Group("/users")
	team.Use(middleware.RequireAuth(cfg), middleware.RequireRole("owner"))
	team.GET("", handler.ListTeam)
	team.POST("", handler.CreateTeamUser)
}
