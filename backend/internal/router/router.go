package router

import (
	"net/http"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/analytics"
	"github.com/businessos/backend/internal/modules/assistant"
	"github.com/businessos/backend/internal/modules/auth"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/notifications"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/purchases"
	"github.com/businessos/backend/internal/modules/reports"
	"github.com/businessos/backend/internal/modules/sales"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func New(db *gorm.DB, cfg *config.Config) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")

	auth.RegisterRoutes(v1, db, cfg)
	business.RegisterRoutes(v1, db, cfg)

	protected := v1.Group("")
	protected.Use(middleware.RequireAuth(cfg))
	{
		inventory.RegisterRoutes(protected, db, cfg)
		products.RegisterRoutes(protected, db, cfg)
		customers.RegisterRoutes(protected, db, cfg)
		suppliers.RegisterRoutes(protected, db, cfg)
		sales.RegisterRoutes(protected, db, cfg)
		purchases.RegisterRoutes(protected, db, cfg)
		finance.RegisterRoutes(protected, db, cfg)
		reports.RegisterRoutes(protected, db, cfg)
		notifications.RegisterRoutes(protected, db, cfg)
		analytics.RegisterRoutes(protected, db, cfg)
		assistant.RegisterRoutes(protected, db, cfg)
	}

	return r
}