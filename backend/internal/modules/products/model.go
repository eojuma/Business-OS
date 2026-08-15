package products

import (
	"time"

	"github.com/google/uuid"
)

type Product struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	Name       string    `gorm:"not null" json:"name"`
	Category   string    `json:"category"`
	Unit       string    `gorm:"not null" json:"unit"`
	Price      int64     `gorm:"not null" json:"price"`
	CostPrice  int64     `gorm:"not null;default:0" json:"cost_price"`
	SKU        string    `json:"sku,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Product) TableName() string {
	return "products"
}
