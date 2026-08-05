package inventory

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	RecordMovement(movement *StockMovement) (*StockLevel, error)
	GetStockLevel(productID, businessID uuid.UUID) (*StockLevel, error)
	ListLowStock(businessID uuid.UUID) ([]StockLevel, error)
	ListMovements(productID, businessID uuid.UUID) ([]StockMovement, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) RecordMovement(movement *StockMovement) (*StockLevel, error) {
	var level StockLevel

	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("product_id = ? AND business_id = ?", movement.ProductID, movement.BusinessID).
			First(&level).Error
		if err == gorm.ErrRecordNotFound {
			level = StockLevel{
				BusinessID: movement.BusinessID,
				ProductID:  movement.ProductID,
				Quantity:   0,
			}
			if err := tx.Create(&level).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		level.Quantity += movement.Quantity
		if err := tx.Save(&level).Error; err != nil {
			return err
		}

		return tx.Create(movement).Error
	})
	if err != nil {
		return nil, err
	}
	return &level, nil
}

func (r *repository) GetStockLevel(productID, businessID uuid.UUID) (*StockLevel, error) {
	var level StockLevel
	if err := r.db.Where("product_id = ? AND business_id = ?", productID, businessID).First(&level).Error; err != nil {
		return nil, err
	}
	return &level, nil
}

func (r *repository) ListLowStock(businessID uuid.UUID) ([]StockLevel, error) {
	var levels []StockLevel
	err := r.db.Where("business_id = ? AND quantity <= low_stock_threshold", businessID).
		Find(&levels).Error
	if err != nil {
		return nil, err
	}
	return levels, nil
}

func (r *repository) ListMovements(productID, businessID uuid.UUID) ([]StockMovement, error) {
	var movements []StockMovement
	err := r.db.Where("product_id = ? AND business_id = ?", productID, businessID).
		Order("created_at desc").Find(&movements).Error
	if err != nil {
		return nil, err
	}
	return movements, nil
}
