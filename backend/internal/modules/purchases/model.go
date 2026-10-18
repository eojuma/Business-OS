package purchases

import (
	"time"

	"github.com/google/uuid"
)

type Purchase struct {
	ID          uuid.UUID          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID  uuid.UUID          `gorm:"type:uuid;index;not null" json:"business_id"`
	SupplierID  uuid.UUID          `gorm:"type:uuid;index;not null" json:"supplier_id"`
	Status      string             `gorm:"not null;default:'draft'" json:"status"`
	TotalAmount int64              `gorm:"not null" json:"total_amount"`
	AmountPaid  int64              `gorm:"not null;default:0" json:"amount_paid"`
	Note        string             `json:"note,omitempty"`
	ExpectedAt  *time.Time         `json:"expected_at,omitempty"`
	ReceivedAt  *time.Time         `json:"received_at,omitempty"`
	DueDate     *time.Time         `json:"due_date,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Items       []PurchaseLineItem `gorm:"foreignKey:PurchaseID" json:"items,omitempty"`
}

func (Purchase) TableName() string { return "purchases" }

type PurchaseLineItem struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PurchaseID  uuid.UUID `gorm:"type:uuid;index;not null" json:"purchase_id"`
	ProductID   uuid.UUID `gorm:"type:uuid;index;not null" json:"product_id"`
	Quantity    int64     `gorm:"not null" json:"quantity"`
	ReceivedQty int64     `gorm:"not null;default:0" json:"received_qty"`
	UnitCost    int64     `gorm:"not null" json:"unit_cost"`
	Subtotal    int64     `gorm:"not null" json:"subtotal"`
	Note        string    `json:"note,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (PurchaseLineItem) TableName() string { return "purchase_line_items" }

// RemainingQty is the quantity still outstanding on the line item.
func (i PurchaseLineItem) RemainingQty() int64 {
	remaining := i.Quantity - i.ReceivedQty
	if remaining < 0 {
		return 0
	}
	return remaining
}

// FullyReceived reports whether the complete ordered quantity has arrived.
func (i PurchaseLineItem) FullyReceived() bool { return i.ReceivedQty >= i.Quantity }

// FullyReceived reports whether every line item has been fully delivered.
func (p Purchase) FullyReceived() bool {
	if len(p.Items) == 0 {
		return false
	}
	for _, item := range p.Items {
		if !item.FullyReceived() {
			return false
		}
	}
	return true
}
