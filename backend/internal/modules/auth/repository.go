package auth

import (
	"errors"
	"time"

	"github.com/businessos/backend/internal/shared/permissions"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrGoogleIDTaken is returned when a Google subject is already linked to a
// different user account.
var ErrGoogleIDTaken = errors.New("this google account is already linked to another user")

// ErrGoogleLinkChanged is returned when an unlink request targets a Google
// subject that is no longer the one linked to the account.
var ErrGoogleLinkChanged = errors.New("the linked google account changed; refresh and try again")

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
	UpdateRoleInBusiness(businessID, userID uuid.UUID, role string) (bool, error)
	SetGoogleID(id uuid.UUID, googleID string) error
	ClaimUnverifiedAccountForGoogle(id uuid.UUID, googleID string) error
	ClearGoogleID(id uuid.UUID) error
	ClearGoogleIDIfMatches(id uuid.UUID, googleID string) error
	MarkEmailVerified(id uuid.UUID) error
	CreateEmailVerificationToken(token *EmailVerificationToken) error
	FindEmailVerificationTokenByHash(hash string) (*EmailVerificationToken, error)
	ConsumeTokenAndMarkEmailVerified(tokenID, userID uuid.UUID) (bool, error)
	DeleteEmailVerificationToken(id uuid.UUID) error
	DeleteEmailVerificationTokensForUser(userID uuid.UUID) error
	DeleteEmailVerificationTokensForUserExcept(userID, keepID uuid.UUID) error
	LatestEmailVerificationTokenCreatedAt(userID uuid.UUID) (time.Time, error)
}

func (r *repository) ListByBusiness(businessID uuid.UUID) ([]User, error) {
	var users []User
	err := r.db.Where("business_id = ?", businessID).Order("created_at asc").Find(&users).Error
	return users, err
}

// UpdateRoleInBusiness changes a user inside the caller's workspace and reports
// whether a row matched. It counts and updates workspace admins in one
// transaction so two concurrent demotions cannot remove the last admin.
func (r *repository) UpdateRoleInBusiness(businessID, userID uuid.UUID, role string) (bool, error) {
	updated := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Lock the admin rows first, in one query, so concurrent demotions
		// cannot both observe two admins and then each remove one.
		var admins []User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("business_id = ? AND role IN ?", businessID, []string{permissions.RoleOwner, permissions.RoleAdmin}).
			Find(&admins).Error; err != nil {
			return err
		}

		var target *User
		for i := range admins {
			if admins[i].ID == userID {
				target = &admins[i]
				break
			}
		}
		if target == nil {
			var user User
			err := tx.Where("id = ? AND business_id = ?", userID, businessID).First(&user).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			target = &user
		}
		if target.Role == role {
			updated = true
			return nil
		}

		if isWorkspaceAdmin(target.Role) && !isWorkspaceAdmin(role) && len(admins) <= 1 {
			return ErrLastAdminRole
		}

		result := tx.Model(&User{}).
			Where("id = ? AND business_id = ?", userID, businessID).
			Update("role", role)
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return updated, nil
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

// ClearGoogleIDIfMatches detaches Google only when the stored subject still
// equals googleID, so a stale unlink cannot clear a subject linked in between.
func (r *repository) ClearGoogleIDIfMatches(id uuid.UUID, googleID string) error {
	result := r.db.Model(&User{}).
		Where("id = ? AND google_id = ?", id, googleID).
		Update("google_id", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrGoogleLinkChanged
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

// LatestEmailVerificationTokenCreatedAt returns when the newest outstanding
// token was created. The resend cooldown is derived from this so it survives
// process restarts.
func (r *repository) LatestEmailVerificationTokenCreatedAt(userID uuid.UUID) (time.Time, error) {
	var createdAt *time.Time
	if err := r.db.Model(&EmailVerificationToken{}).
		Where("user_id = ?", userID).
		Select("MAX(created_at)").
		Scan(&createdAt).Error; err != nil {
		return time.Time{}, err
	}
	if createdAt == nil {
		return time.Time{}, gorm.ErrRecordNotFound
	}
	return *createdAt, nil
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
