package payments

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterRoutes mounts public webhooks and protected payments endpoints.
func RegisterRoutes(public *gin.RouterGroup, protected *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	client := NewMpesaClient(cfg)

	var invSvc invoices.Service
	if db != nil {
		invSvc = invoices.NewService(invoices.NewRepository(db))
	}
	var custSvc customers.Service
	if db != nil {
		custSvc = customers.NewService(customers.NewRepository(db))
	}
	var finSvc finance.Service
	if db != nil {
		finSvc = finance.NewService(finance.NewRepository(db))
	}

	svc := NewService(repo, client, cfg, invSvc, custSvc, finSvc)
	h := NewHandler(svc)

	// Public M-Pesa webhooks (called by Safaricom without user JWT)
	callbacks := public.Group("/payments/mpesa/callback")
	{
		callbacks.POST("/stk", h.HandleSTKCallback)
		callbacks.POST("/c2b-validate", h.HandleC2BValidate)
		callbacks.POST("/c2b-confirm", h.HandleC2BConfirm)
		callbacks.POST("/b2c-result", h.HandleB2CResult)
		callbacks.POST("/b2c-timeout", h.HandleB2CTimeout)
	}

	// Protected application routes
	p := protected.Group("/payments")
	{
		p.POST("/mpesa/stk-push", h.InitiateSTKPush)
		p.POST("/mpesa/b2c", h.InitiateB2C)
		p.GET("/mpesa/transactions", h.ListMpesaTransactions)

		p.POST("/bank/manual", h.RecordManualBank)
		p.POST("/bank/statement", h.ImportBankStatement)
		p.GET("/bank/transactions", h.ListBankTransactions)

		p.GET("/review-queue", h.ListReviewQueue)
		p.POST("/review-queue/:id/reconcile", h.ReconcileReviewItem)
	}
}
