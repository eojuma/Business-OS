package sales

import (
	"time"

	"github.com/google/uuid"
)

type Sale struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID  uuid.UUID  `gorm:"type:uuid;index;not null" json:"business_id"`
	CustomerID  *uuid.UUID `gorm:"type:uuid;index" json:"customer_id,omitempty"` // nil = walk-in/cash sale; customers module doesn't exist yet, so this stays optional
	TotalAmount int64      `gorm:"not null" json:"total_amount"`                  // cents, sum of line items
	Discount    int64      `gorm:"not null;default:0" json:"discount"`            // cents
	Note        string     `json:"note,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`

	LineItems []SaleLineItem `gorm:"foreignKey:SaleID" json:"line_items,omitempty"`
}

func (Sale) TableName() string {
	return "sales"
}


type SaleLineItem struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	SaleID    uuid.UUID `gorm:"type:uuid;index;not null" json:"sale_id"`
	ProductID uuid.UUID `gorm:"type:uuid;index;not null" json:"product_id"`
	Quantity  int64     `gorm:"not null" json:"quantity"`
	UnitPrice int64     `gorm:"not null" json:"unit_price"` 
	Subtotal  int64     `gorm:"not null" json:"subtotal"`  
	CreatedAt time.Time `json:"created_at"`
}

func (SaleLineItem) TableName() string {
	return "sale_line_items"
}