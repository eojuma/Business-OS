package sales

import (
	"errors"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// inventoryAdapter wraps inventory.Repository so it satisfies sales's
// InventoryMover interface, translating inventory's own
// ErrInsufficientStock into sales's version.
type inventoryAdapter struct {
	repo inventory.Repository
}

func (a *inventoryAdapter) RecordMovementTx(tx *gorm.DB, businessID, productID uuid.UUID, movementType string, quantity int64, note string) error {
	err := a.repo.RecordMovementTx(tx, businessID, productID, movementType, quantity, note)
	if errors.Is(err, inventory.ErrInsufficientStock) {
		return ErrInsufficientStock
	}
	return err
}


type customerAdapter struct {
	svc customers.Service
}

func (a *customerAdapter) ChargeCreditTx(tx *gorm.DB, businessID, customerID uuid.UUID, amount int64) error {
	err := a.svc.ChargeCreditTx(tx, businessID, customerID, amount)
	if errors.Is(err, customers.ErrCreditLimitExceeded) {
		return ErrCreditLimitExceeded
	}
	return err
}

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	inventoryRepo := inventory.NewRepository(db)
	productsRepo := products.NewRepository(db)
	customersRepo := customers.NewRepository(db)
	customersSvc := customers.NewService(customersRepo)

	inventoryMover := &inventoryAdapter{repo: inventoryRepo}
	customerCharger := &customerAdapter{svc: customersSvc}

	svc := NewService(repo, inventoryMover, productsRepo, customerCharger)
	handler := NewHandler(svc)

	group := rg.Group("/sales")
	{
		group.POST("", handler.Create)
		group.GET("", handler.List)
		group.GET("/:id", handler.Get)
	}
}

func NewInventoryAdapter(repo inventory.Repository) InventoryMover {
	return &inventoryAdapter{repo: repo}
}

func NewCustomerAdapter(svc customers.Service) CustomerCharger {
	return &customerAdapter{svc: svc}
}