package invoices

import (
	"time"

	"github.com/businessos/backend/internal/modules/customers"
	"github.com/google/uuid"
)

// Status is the settlement state of an invoice.
type Status string

const (
	StatusUnpaid  Status = "unpaid"
	StatusPartial Status = "partial"
	StatusPaid    Status = "paid"
	StatusOverdue Status = "overdue"
)

// Invoice is the receivable document created for a completed sale.
type Invoice struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID    uuid.UUID  `gorm:"type:uuid;uniqueIndex:idx_invoices_business_number,priority:1;not null" json:"business_id"`
	SaleID        uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"sale_id"`
	InvoiceNumber string     `gorm:"not null;uniqueIndex:idx_invoices_business_number,priority:2" json:"invoice_number"`
	CustomerID    *uuid.UUID `gorm:"type:uuid;index" json:"customer_id,omitempty"`
	Status        Status     `gorm:"not null;default:'unpaid'" json:"status"`
	IssueDate     time.Time  `gorm:"not null" json:"issue_date"`
	DueDate       time.Time  `gorm:"not null" json:"due_date"`
	Subtotal      int64      `gorm:"not null" json:"subtotal"`
	Discount      int64      `gorm:"not null;default:0" json:"discount"`
	TotalAmount   int64      `gorm:"not null" json:"total_amount"`
	AmountPaid    int64      `gorm:"not null;default:0" json:"amount_paid"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	Customer         *customers.Customer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Payments         []InvoicePayment    `gorm:"foreignKey:InvoiceID" json:"payments,omitempty"`
	RemainingBalance int64               `gorm:"-" json:"remaining_balance"`
	IsOverdue        bool                `gorm:"-" json:"is_overdue"`
}

func (Invoice) TableName() string { return "invoices" }

// InvoicePayment is one settlement applied against an invoice.
type InvoicePayment struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	InvoiceID  uuid.UUID `gorm:"type:uuid;index;not null" json:"invoice_id"`
	Amount     int64     `gorm:"not null" json:"amount"`
	Note       string    `json:"note,omitempty"`
	PaidAt     time.Time `gorm:"not null" json:"paid_at"`
	CreatedAt  time.Time `json:"created_at"`
}

func (InvoicePayment) TableName() string { return "invoice_payments" }

// StatusFor derives the settlement status from the invoice total and payments.
func StatusFor(total, paid int64) Status {
	switch {
	case paid >= total:
		return StatusPaid
	case paid > 0:
		return StatusPartial
	default:
		return StatusUnpaid
	}
}

// Compute fills the derived remaining balance and overdue flag.
func (i *Invoice) Compute(now time.Time) {
	remaining := i.TotalAmount - i.AmountPaid
	if remaining < 0 {
		remaining = 0
	}
	i.RemainingBalance = remaining
	i.IsOverdue = i.Status != StatusPaid && i.DueDate.Before(now)
}
