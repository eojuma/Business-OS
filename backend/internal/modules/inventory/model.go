package inventory

import (
	"time"

	"github.com/google/uuid"
)

type StockLevel struct {
	ID                uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID        uuid.UUID `gorm:"type:uuid;index;not null" json:"business_id"`
	ProductID         uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"product_id"`
	Quantity          int64     `gorm:"not null;default:0" json:"quantity"`
	LowStockThreshold int64     `gorm:"not null;default:0" json:"low_stock_threshold"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (StockLevel) TableName() string {
	return "stock_levels"
}

func (s StockLevel) IsLowStock() bool {
	return s.Quantity <= s.LowStockThreshold
}

type MovementType string

const (
	MovementRestock    MovementType = "restock"
	MovementSale       MovementType = "sale"
	MovementAdjustment MovementType = "adjustment"
	MovementReturn     MovementType = "return"
)

type StockMovement struct {
	ID         uuid.UUID    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID uuid.UUID    `gorm:"type:uuid;index;not null" json:"business_id"`
	ProductID  uuid.UUID    `gorm:"type:uuid;index;not null" json:"product_id"`
	Type       MovementType `gorm:"not null" json:"type"`
	Quantity   int64        `gorm:"not null" json:"quantity"` // signed: +in, -out
	Note       string       `json:"note,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

func (StockMovement) TableName() string {
	return "stock_movements"
}
