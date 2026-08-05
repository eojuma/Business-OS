package sales

import (
	"errors"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

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

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	repo := NewRepository(db)
	inventoryRepo := inventory.NewRepository(db)
	productsRepo := products.NewRepository(db)

	inventoryMover := &inventoryAdapter{repo: inventoryRepo}
	svc := NewService(repo, inventoryMover, productsRepo)
	handler := NewHandler(svc)

	group := rg.Group("/sales")
	{
		group.POST("", handler.Create)
		group.GET("", handler.List)
		group.GET("/:id", handler.Get)
	}
}
