package suppliers

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Supplier struct {
	ID                 uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID         uuid.UUID      `gorm:"type:uuid;index;not null" json:"business_id"`
	Name               string         `gorm:"not null" json:"name"`
	Phone              string         `json:"phone"`
	Email              string         `json:"email,omitempty"`
	Address            string         `json:"address,omitempty"`
	OutstandingBalance int64          `gorm:"not null;default:0" json:"outstanding_balance"`
	PaymentTerms       string         `gorm:"not null;default:'net30'" json:"payment_terms"`
	DueDate            *time.Time     `gorm:"type:timestamptz" json:"due_date,omitempty"`
	LastReceivedAt     *time.Time     `gorm:"type:timestamptz" json:"last_received_at,omitempty"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
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

type AgingBucket string

const (
	AgingCurrent    AgingBucket = "current"
	AgingDays1To30  AgingBucket = "days_1_30"
	AgingDays31To60 AgingBucket = "days_31_60"
	AgingDays61To90 AgingBucket = "days_61_90"
	AgingDays90Plus AgingBucket = "days_90_plus"
)

type AgingSupplier struct {
	ID                 uuid.UUID   `json:"id"`
	Name               string      `json:"name"`
	OutstandingBalance int64       `json:"outstanding_balance"`
	DueDate            *time.Time  `json:"due_date"`
	DaysOverdue        int         `json:"days_overdue"`
	Bucket             AgingBucket `json:"-"`
}

// AgingReport ages current balances against each supplier's saved due date.
// AsOf is an aging reference, not a historical balance snapshot. Day boundaries
// are UTC; suppliers without a due date are included in Current.
type AgingReport struct {
	AsOf       time.Time             `json:"as_of"`
	Current    []AgingSupplier       `json:"current"`
	Days1To30  []AgingSupplier       `json:"days_1_30"`
	Days31To60 []AgingSupplier       `json:"days_31_60"`
	Days61To90 []AgingSupplier       `json:"days_61_90"`
	Days90Plus []AgingSupplier       `json:"days_90_plus"`
	Totals     map[AgingBucket]int64 `json:"totals"`
}
