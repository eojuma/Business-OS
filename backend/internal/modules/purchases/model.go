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
	ReceivedAt  *time.Time         `json:"received_at,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Items       []PurchaseLineItem `gorm:"foreignKey:PurchaseID" json:"items,omitempty"`
}

func (Purchase) TableName() string { return "purchases" }

type PurchaseLineItem struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PurchaseID uuid.UUID `gorm:"type:uuid;index;not null" json:"purchase_id"`
	ProductID  uuid.UUID `gorm:"type:uuid;index;not null" json:"product_id"`
	Quantity   int64     `gorm:"not null" json:"quantity"`
	UnitCost   int64     `gorm:"not null" json:"unit_cost"`
	Subtotal   int64     `gorm:"not null" json:"subtotal"`
	CreatedAt  time.Time `json:"created_at"`
}

func (PurchaseLineItem) TableName() string { return "purchase_line_items" }
