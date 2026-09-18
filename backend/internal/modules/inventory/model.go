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

// MovementDirection lets an adjustment (or return) move stock either way.
// Restock is always incoming; sale is always outgoing.
type MovementDirection string

const (
	DirectionIn  MovementDirection = "in"
	DirectionOut MovementDirection = "out"
)

// SignedQuantity returns the signed stock delta for a movement. It is the
// single place that decides whether a movement adds or removes stock, so the
// HTTP path and the sales transaction path cannot disagree.
func SignedQuantity(t MovementType, direction MovementDirection, quantity int64) int64 {
	if quantity < 0 {
		quantity = -quantity
	}

	outgoing := t == MovementSale ||
		((t == MovementAdjustment || t == MovementReturn) && direction == DirectionOut)

	if outgoing {
		return -quantity
	}
	return quantity
}

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
