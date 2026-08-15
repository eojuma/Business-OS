package customers

import (
	"time"

	"github.com/google/uuid"
)

type Customer struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID  uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	Name        string    `gorm:"not null" json:"name"`
	Phone       string    `json:"phone"`
	Email       string    `json:"email,omitempty"`
	Balance     int64     `gorm:"not null;default:0" json:"balance"`      // cents, amount currently owed
	CreditLimit int64     `gorm:"not null;default:0" json:"credit_limit"` // cents, 0 = no credit allowed
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Customer) TableName() string {
	return "customers"
}

func (c Customer) HasAvailableCredit(amount int64) bool {
	return c.Balance+amount <= c.CreditLimit
}

type Payment struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	CustomerID uuid.UUID `gorm:"type:uuid;index;not null" json:"customer_id"`
	Amount     int64     `gorm:"not null;check:amount > 0" json:"amount"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Payment) TableName() string { return "customer_payments" }
