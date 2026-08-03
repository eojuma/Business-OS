package business

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	Create(b *Business) error
	FindByID(id uuid.UUID) (*Business, error)
	Update(b *Business) error
	Exists(id uuid.UUID) (bool, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Create(b *Business) error {
	return r.db.Create(b).Error
}

func (r *repository) FindByID(id uuid.UUID) (*Business, error) {
	var b Business
	if err := r.db.Where("id = ?", id).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *repository) Update(b *Business) error {
	return r.db.Save(b).Error
}

func (r *repository) Exists(id uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&Business{}).Where("id = ?", id).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
