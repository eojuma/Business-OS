package finance

import (
	"time"

	"github.com/google/uuid"
)

type Expense struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID  uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	Category    string    `gorm:"not null" json:"category"`
	Description string    `json:"description,omitempty"`
	Amount      int64     `gorm:"not null" json:"amount"`
	IncurredAt  time.Time `gorm:"not null" json:"incurred_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Expense) TableName() string { return "expenses" }
