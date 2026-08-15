package products

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository isolates DB access for the products module — same pattern
// as auth and business.
type Repository interface {
	Create(p *Product) error
	FindByID(id, businessID uuid.UUID) (*Product, error)
	List(businessID uuid.UUID) ([]Product, error)
	Update(p *Product) error
	Delete(id, businessID uuid.UUID) error
	GetPricing(businessID, productID uuid.UUID) (price, costPrice int64, err error)
	UpdateCostPriceTx(tx *gorm.DB, businessID, productID uuid.UUID, costPrice int64) error
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Create(p *Product) error {
	return r.db.Create(p).Error
}

func (r *repository) FindByID(id, businessID uuid.UUID) (*Product, error) {
	var p Product
	if err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *repository) GetPricing(businessID, productID uuid.UUID) (int64, int64, error) {
	var p Product
	err := r.db.Select("price", "cost_price").
		Where("id = ? AND business_id = ?", productID, businessID).
		First(&p).Error
	if err != nil {
		return 0, 0, err
	}
	return p.Price, p.CostPrice, nil
}

func (r *repository) UpdateCostPriceTx(tx *gorm.DB, businessID, productID uuid.UUID, costPrice int64) error {
	result := tx.Model(&Product{}).Where("id = ? AND business_id = ?", productID, businessID).Update("cost_price", costPrice)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *repository) List(businessID uuid.UUID) ([]Product, error) {
	var products []Product
	if err := r.db.Where("business_id = ?", businessID).Order("created_at desc").Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

func (r *repository) Update(p *Product) error {
	return r.db.Save(p).Error
}

func (r *repository) Delete(id, businessID uuid.UUID) error {
	return r.db.Where("id = ? AND business_id = ?", id, businessID).Delete(&Product{}).Error
}
