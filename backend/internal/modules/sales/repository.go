package sales

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InventoryMover interface {
	RecordMovementTx(tx *gorm.DB, businessID, productID uuid.UUID, movementType string, quantity int64, note string) error
}

type Repository interface {
	CreateSale(sale *Sale, lineItems []SaleLineItem, inventory InventoryMover) error
	FindByID(id, businessID uuid.UUID) (*Sale, error)
	List(businessID uuid.UUID) ([]Sale, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) CreateSale(sale *Sale, lineItems []SaleLineItem, inventory InventoryMover) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(sale).Error; err != nil {
			return err
		}

		for i := range lineItems {
			lineItems[i].SaleID = sale.ID
			if err := tx.Create(&lineItems[i]).Error; err != nil {
				return err
			}

			err := inventory.RecordMovementTx(
				tx,
				sale.BusinessID,
				lineItems[i].ProductID,
				"sale",
				lineItems[i].Quantity,
				"sale "+sale.ID.String(),
			)
			if err != nil {
				return err 
			}
		}

		return nil
	})
}

func (r *repository) FindByID(id, businessID uuid.UUID) (*Sale, error) {
	var sale Sale
	err := r.db.Preload("LineItems").
		Where("id = ? AND business_id = ?", id, businessID).
		First(&sale).Error
	if err != nil {
		return nil, err
	}
	return &sale, nil
}

func (r *repository) List(businessID uuid.UUID) ([]Sale, error) {
	var sales []Sale
	err := r.db.Preload("LineItems").
		Where("business_id = ?", businessID).
		Order("created_at desc").
		Find(&sales).Error
	if err != nil {
		return nil, err
	}
	return sales, nil
}