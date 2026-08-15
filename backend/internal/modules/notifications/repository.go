package notifications

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Notification struct {
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	ProductID   uuid.UUID `json:"product_id"`
	ProductName string    `json:"product_name"`
	Quantity    int64     `json:"quantity"`
	Threshold   int64     `json:"threshold"`
	Message     string    `json:"message"`
}
type Repository interface {
	LowStock(uuid.UUID) ([]Notification, error)
}
type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }
func (r *repository) LowStock(businessID uuid.UUID) ([]Notification, error) {
	var items []Notification
	err := r.db.Table("stock_levels sl").Select("'low_stock' AS type, CASE WHEN sl.quantity <= 0 THEN 'critical' ELSE 'warning' END AS severity, sl.product_id, p.name AS product_name, sl.quantity, sl.low_stock_threshold AS threshold, CONCAT(p.name, ' has ', sl.quantity, ' ', p.unit, ' remaining') AS message").Joins("JOIN products p ON p.id = sl.product_id AND p.business_id = sl.business_id").Where("sl.business_id = ? AND sl.quantity <= sl.low_stock_threshold", businessID).Order("sl.quantity asc").Scan(&items).Error
	return items, err
}
