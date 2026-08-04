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