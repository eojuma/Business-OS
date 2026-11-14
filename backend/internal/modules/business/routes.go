package business

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/audit"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterRoutes registers the business module HTTP routes.
// Public: POST /business (create)
// Authenticated: GET /business, PATCH /business
func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)

	group := rg.Group("/business")
	{
		// The public create route is audited without an authenticated actor.
		// audit.Middleware derives the new business id from the response.
		group.POST("", audit.Middleware(db), handler.Create)

		protected := group.Group("")
		protected.Use(middleware.RequireAuth(cfg, middleware.RoleFromDB(db)), middleware.RequireRoutePermission(), audit.Middleware(db))
		{
			protected.GET("", handler.Get)
			protected.PATCH("", handler.Update)
		}
	}
}
