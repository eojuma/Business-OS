package business

import (
	"time"

	"github.com/google/uuid"
)

type Business struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name        string     `gorm:"not null" json:"name"`
	Phone       string     `json:"phone"`
	Email       string     `json:"email"`
	Address     string     `json:"address"`
	Currency    string     `gorm:"default:'KES'" json:"currency"`
	OwnerUserID *uuid.UUID `gorm:"type:uuid" json:"owner_user_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Business) TableName() string {
	return "businesses"
}
