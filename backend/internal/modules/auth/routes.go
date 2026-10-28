package auth

import (
	"context"
	"log"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// businessOwnerCreator creates the workspace and its owner atomically so a
// failed user insert cannot leave an orphan business behind.
type businessOwnerCreator struct{ db *gorm.DB }

func (c *businessOwnerCreator) CreateWithOwner(name, phone string, user *User) error {
	return c.db.Transaction(func(tx *gorm.DB) error {
		b := &business.Business{Name: name, Phone: phone}
		if err := tx.Create(b).Error; err != nil {
			return err
		}
		user.BusinessID = b.ID
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return tx.Model(&business.Business{}).Where("id = ?", b.ID).Update("owner_user_id", user.ID).Error
	})
}

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	businessRepo := business.NewRepository(db)

	googleVerifier, err := NewGoogleTokenVerifier(context.Background(), cfg.GoogleClientID)
	if err != nil {
		log.Printf("auth: google sign-in disabled: %v", err)
	} else if googleVerifier == nil {
		log.Printf("auth: google sign-in disabled: GOOGLE_CLIENT_ID is not set")
	}

	svc := NewService(repo, cfg, businessRepo, googleVerifier, &businessOwnerCreator{db: db})
	handler := NewHandler(svc)

	authGroup := rg.Group("/auth")
	{
		authGroup.POST("/register", handler.Register)
		authGroup.POST("/login", handler.Login)
		authGroup.POST("/google", handler.Google)
	}
	authGroup.GET("/me", middleware.RequireAuth(cfg), handler.Me)
	team := rg.Group("/users")
	team.Use(middleware.RequireAuth(cfg), middleware.RequireRole("owner"))
	team.GET("", handler.ListTeam)
	team.POST("", handler.CreateTeamUser)
}
