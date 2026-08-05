package customers

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	Create(c *Customer) error
	FindByID(id, businessID uuid.UUID) (*Customer, error)
	List(businessID uuid.UUID) ([]Customer, error)
	Update(c *Customer) error
	ListAboveBalance(businessID uuid.UUID, threshold int64) ([]Customer, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Create(c *Customer) error {
	return r.db.Create(c).Error
}

func (r *repository) FindByID(id, businessID uuid.UUID) (*Customer, error) {
	var c Customer
	if err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *repository) List(businessID uuid.UUID) ([]Customer, error) {
	var customers []Customer
	if err := r.db.Where("business_id = ?", businessID).Order("name asc").Find(&customers).Error; err != nil {
		return nil, err
	}
	return customers, nil
}

func (r *repository) Update(c *Customer) error {
	return r.db.Save(c).Error
}

func (r *repository) ListAboveBalance(businessID uuid.UUID, threshold int64) ([]Customer, error) {
	var customers []Customer
	err := r.db.Where("business_id = ? AND balance > ?", businessID, threshold).
		Order("balance desc").Find(&customers).Error
	if err != nil {
		return nil, err
	}
	return customers, nil
}
