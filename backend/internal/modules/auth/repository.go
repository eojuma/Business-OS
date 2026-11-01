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

// errTokenNotConsumed rolls back the verification transaction without changing
// either row.
var errTokenNotConsumed = errors.New("email verification token not consumed")

type Repository interface {
	Create(user *User) error
	DeleteUser(id uuid.UUID) error
	FindByEmail(email string) (*User, error)
	FindByGoogleID(googleID string) (*User, error)
	FindByID(id uuid.UUID) (*User, error)
	ListByBusiness(businessID uuid.UUID) ([]User, error)
	SetGoogleID(id uuid.UUID, googleID string) error
	ClaimUnverifiedAccountForGoogle(id uuid.UUID, googleID string) error
	ClearGoogleID(id uuid.UUID) error
	MarkEmailVerified(id uuid.UUID) error
	CreateEmailVerificationToken(token *EmailVerificationToken) error
	FindEmailVerificationTokenByHash(hash string) (*EmailVerificationToken, error)
	ConsumeTokenAndMarkEmailVerified(tokenID, userID uuid.UUID) (bool, error)
	DeleteEmailVerificationToken(id uuid.UUID) error
	DeleteEmailVerificationTokensForUser(userID uuid.UUID) error
	DeleteEmailVerificationTokensForUserExcept(userID, keepID uuid.UUID) error
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

func (r *repository) DeleteUser(id uuid.UUID) error {
	result := r.db.Where("id = ?", id).Delete(&User{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
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
	// The google_id IS NULL guard keeps two concurrent links from silently
	// replacing each other's subject.
	result := r.db.Model(&User{}).
		Where("id = ? AND google_id IS NULL", id).
		Update("google_id", googleID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			return ErrGoogleIDTaken
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.missingOrLinked(id)
	}
	return nil
}

// ClaimUnverifiedAccountForGoogle links a Google subject to an unverified local
// account, marks the address verified, and clears the untrusted password in a
// single conditional update. A concurrent claim or verification leaves the row
// untouched.
func (r *repository) ClaimUnverifiedAccountForGoogle(id uuid.UUID, googleID string) error {
	result := r.db.Model(&User{}).
		Where("id = ? AND google_id IS NULL AND email_verified = ?", id, false).
		Updates(map[string]any{
			"google_id":      googleID,
			"email_verified": true,
			"password_hash":  nil,
		})
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			return ErrGoogleIDTaken
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return r.missingOrLinked(id)
	}
	return nil
}

// missingOrLinked distinguishes a vanished user from a subject that was
// claimed by another request between the read and the conditional update.
func (r *repository) missingOrLinked(id uuid.UUID) error {
	var count int64
	if err := r.db.Model(&User{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrGoogleIDTaken
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

// ConsumeTokenAndMarkEmailVerified marks the token used and the user verified in
// one transaction. It reports false, and changes neither row, when the token is
// missing, already used, or expired.
func (r *repository) ConsumeTokenAndMarkEmailVerified(tokenID, userID uuid.UUID) (bool, error) {
	now := time.Now()
	err := r.db.Transaction(func(tx *gorm.DB) error {
		consume := tx.Model(&EmailVerificationToken{}).
			Where("id = ? AND used_at IS NULL AND expires_at > ?", tokenID, now).
			Update("used_at", now)
		if consume.Error != nil {
			return consume.Error
		}
		if consume.RowsAffected != 1 {
			return errTokenNotConsumed
		}
		mark := tx.Model(&User{}).Where("id = ?", userID).Update("email_verified", true)
		if mark.Error != nil {
			return mark.Error
		}
		if mark.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if errors.Is(err, errTokenNotConsumed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *repository) DeleteEmailVerificationToken(id uuid.UUID) error {
	return r.db.Where("id = ?", id).Delete(&EmailVerificationToken{}).Error
}

func (r *repository) DeleteEmailVerificationTokensForUser(userID uuid.UUID) error {
	return r.db.Where("user_id = ?", userID).Delete(&EmailVerificationToken{}).Error
}

// DeleteEmailVerificationTokensForUserExcept removes every outstanding token for
// a user except the one that was just delivered, so older links stop working
// without risking the newest one.
func (r *repository) DeleteEmailVerificationTokensForUserExcept(userID, keepID uuid.UUID) error {
	return r.db.Where("user_id = ? AND id <> ?", userID, keepID).Delete(&EmailVerificationToken{}).Error
}
