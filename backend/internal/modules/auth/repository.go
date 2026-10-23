package auth

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	Create(user *User) error
	FindByEmail(email string) (*User, error)
	FindByGoogleID(googleID string) (*User, error)
	FindByID(id uuid.UUID) (*User, error)
	ListByBusiness(businessID uuid.UUID) ([]User, error)
	SetGoogleID(id uuid.UUID, googleID string) error
}

func (r *repository) ListByBusiness(businessID uuid.UUID) ([]User, error) {
	var users []User
	err := r.db.Where("business_id = ?", businessID).Order("created_at asc").Find(&users).Error
	return users, err
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Create(user *User) error {
	return r.db.Create(user).Error
}

func (r *repository) FindByEmail(email string) (*User, error) {
	var user User
	if err := r.db.Where("LOWER(email) = LOWER(?)", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *repository) FindByGoogleID(googleID string) (*User, error) {
	var user User
	if err := r.db.Where("google_id = ?", googleID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *repository) FindByID(id uuid.UUID) (*User, error) {
	var user User
	if err := r.db.Where("id = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *repository) SetGoogleID(id uuid.UUID, googleID string) error {
	result := r.db.Model(&User{}).Where("id = ?", id).Update("google_id", googleID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
