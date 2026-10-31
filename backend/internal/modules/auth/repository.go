package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrGoogleIDTaken is returned when a Google subject is already linked to a
// different user account.
var ErrGoogleIDTaken = errors.New("this google account is already linked to another user")

type Repository interface {
	Create(user *User) error
	FindByEmail(email string) (*User, error)
	FindByGoogleID(googleID string) (*User, error)
	FindByID(id uuid.UUID) (*User, error)
	ListByBusiness(businessID uuid.UUID) ([]User, error)
	SetGoogleID(id uuid.UUID, googleID string) error
	ClearGoogleID(id uuid.UUID) error
	ClearPassword(id uuid.UUID) error
	MarkEmailVerified(id uuid.UUID) error
	CreateEmailVerificationToken(token *EmailVerificationToken) error
	FindEmailVerificationTokenByHash(hash string) (*EmailVerificationToken, error)
	ConsumeEmailVerificationToken(id uuid.UUID) (bool, error)
	DeleteEmailVerificationTokensForUser(userID uuid.UUID) error
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
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			return ErrGoogleIDTaken
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *repository) ClearGoogleID(id uuid.UUID) error {
	result := r.db.Model(&User{}).Where("id = ?", id).Update("google_id", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *repository) MarkEmailVerified(id uuid.UUID) error {
	result := r.db.Model(&User{}).Where("id = ?", id).Update("email_verified", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *repository) ClearPassword(id uuid.UUID) error {
	result := r.db.Model(&User{}).Where("id = ?", id).Update("password_hash", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *repository) CreateEmailVerificationToken(token *EmailVerificationToken) error {
	return r.db.Create(token).Error
}

func (r *repository) FindEmailVerificationTokenByHash(hash string) (*EmailVerificationToken, error) {
	var token EmailVerificationToken
	if err := r.db.Where("token_hash = ?", hash).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

// ConsumeEmailVerificationToken marks a token as used atomically. It reports
// false when the token was already consumed, so a link cannot be replayed.
func (r *repository) ConsumeEmailVerificationToken(id uuid.UUID) (bool, error) {
	now := time.Now()
	result := r.db.Model(&EmailVerificationToken{}).
		Where("id = ? AND used_at IS NULL", id).
		Update("used_at", now)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *repository) DeleteEmailVerificationTokensForUser(userID uuid.UUID) error {
	return r.db.Where("user_id = ?", userID).Delete(&EmailVerificationToken{}).Error
}
