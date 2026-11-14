package auth

import (
	"context"
	"log"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/audit"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/shared/email"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// DeleteWorkspace removes a workspace whose sign-up could not deliver a
// verification email. Users cascade with the business row.
func (c *businessOwnerCreator) DeleteWorkspace(businessID uuid.UUID) error {
	return c.db.Where("id = ?", businessID).Delete(&business.Business{}).Error
}

// RegisterRoutes wires auth, account, and team endpoints and returns the drain hook.
func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) func(context.Context) {
	repo := NewRepository(db)

	googleVerifier, err := NewGoogleTokenVerifier(context.Background(), cfg.GoogleClientID)
	if err != nil {
		log.Printf("auth: google sign-in disabled: %v", err)
	} else if googleVerifier == nil {
		log.Printf("auth: google sign-in disabled: GOOGLE_CLIENT_ID is not set")
	}

	svc := NewService(repo, cfg, googleVerifier, &businessOwnerCreator{db: db}, email.NewFromConfig(cfg))
	handler := NewHandler(svc)

	authGroup := rg.Group("/auth")
	{
		authGroup.POST("/register", handler.Register)
		authGroup.POST("/login", handler.Login)
		authGroup.POST("/google", handler.Google)
		authGroup.POST("/email/verify", handler.VerifyEmail)
		authGroup.POST("/email/resend", handler.ResendVerification)
	}
	authGroup.GET("/me", middleware.RequireAuth(cfg, middleware.RoleFromDB(db)), middleware.RequireRoutePermission(), handler.Me)
	linkGroup := rg.Group("/auth")
	linkGroup.Use(middleware.RequireAuth(cfg, middleware.RoleFromDB(db)), middleware.RequireRoutePermission())
	linkGroup.POST("/google/link", handler.LinkGoogle)
	linkGroup.POST("/google/unlink", handler.UnlinkGoogle)
	team := rg.Group("/users")
	team.Use(middleware.RequireAuth(cfg, middleware.RoleFromDB(db)), middleware.RequireRoutePermission(), audit.Middleware(db))
	team.GET("", handler.ListTeam)
	team.POST("", handler.CreateTeamUser)
	team.PATCH("/:id/role", handler.UpdateTeamMemberRole)

	// Returned so the process can drain background verification sends on
	// shutdown.
	return svc.WaitBackground
}
