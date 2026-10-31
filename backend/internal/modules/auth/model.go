package auth

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID   uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	Name         string    `gorm:"not null" json:"name"`
	Email        string    `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash *string   `gorm:"type:text" json:"-"`
	GoogleID     *string   `gorm:"type:text" json:"-"`
	// GoogleLinked mirrors GoogleID for clients without exposing the subject.
	GoogleLinked  bool      `gorm:"-" json:"google_linked"`
	Role          string    `gorm:"not null;default:'owner'" json:"role"`
	Active        bool      `gorm:"default:true" json:"active"`
	// No GORM default here: a `default:true` tag would make GORM omit the zero
	// value on insert, letting the DB default verify new sign-ups. The
	// column-level default only grandfathers rows added by the migration.
	EmailVerified bool      `gorm:"not null" json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (User) TableName() string {
	return "users"
}

// EmailVerificationToken is a single-use, expiring token that proves ownership
// of a user's email address. Only the hash is persisted.
type EmailVerificationToken struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID  `gorm:"type:uuid;index;not null" json:"user_id"`
	TokenHash string     `gorm:"type:text;not null;uniqueIndex" json:"-"`
	ExpiresAt time.Time  `gorm:"not null" json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

func (EmailVerificationToken) TableName() string {
	return "email_verification_tokens"
}
