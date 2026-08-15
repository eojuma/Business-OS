package suppliers

import (
	"time"

	"github.com/google/uuid"
)

type Supplier struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID         uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	Name               string    `gorm:"not null" json:"name"`
	Phone              string    `json:"phone"`
	Email              string    `json:"email,omitempty"`
	Address            string    `json:"address,omitempty"`
	OutstandingBalance int64     `gorm:"not null;default:0" json:"outstanding_balance"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (Supplier) TableName() string { return "suppliers" }

type Payment struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	SupplierID uuid.UUID `gorm:"type:uuid;index;not null" json:"supplier_id"`
	Amount     int64     `gorm:"not null" json:"amount"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Payment) TableName() string { return "supplier_payments" }
