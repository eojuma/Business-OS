package analytics

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Overview struct {
	Revenue        int64 `json:"revenue"`
	Profit         int64 `json:"profit"`
	SaleCount      int64 `json:"sale_count"`
	AverageSale    int64 `json:"average_sale"`
	UnitsSold      int64 `json:"units_sold"`
	CustomerCredit int64 `json:"customer_credit"`
	SupplierDebt   int64 `json:"supplier_debt"`
	LowStockCount  int64 `json:"low_stock_count"`
}
type ProductPerformance struct {
	ProductID    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	QuantitySold int64     `json:"quantity_sold"`
	Revenue      int64     `json:"revenue"`
	Profit       int64     `json:"profit"`
}
type SlowMovingProduct struct {
	ProductID      uuid.UUID  `json:"product_id"`
	ProductName    string     `json:"product_name"`
	QuantityOnHand int64      `json:"quantity_on_hand"`
	LastSoldAt     *time.Time `json:"last_sold_at,omitempty"`
	DaysSinceSale  *int64     `json:"days_since_sale,omitempty"`
}
type Repository interface {
	Overview(uuid.UUID, time.Time, time.Time) (*Overview, error)
	TopProducts(uuid.UUID, time.Time, time.Time, int) ([]ProductPerformance, error)
	SlowMoving(uuid.UUID, time.Time) ([]SlowMovingProduct, error)
}
type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }
func (r *repository) Overview(id uuid.UUID, from, to time.Time) (*Overview, error) {
	var out Overview
	if err := r.db.Table("sales").Select("COALESCE(SUM(total_amount),0) revenue, COUNT(*) sale_count").Where("business_id = ? AND created_at BETWEEN ? AND ?", id, from, to).Scan(&out).Error; err != nil {
		return nil, err
	}
	var costOfGoods int64
	if err := r.db.Table("sale_line_items li").Select("COALESCE(SUM(li.quantity),0) units_sold, COALESCE(SUM(li.quantity * li.unit_cost),0) cost_of_goods").Joins("JOIN sales s ON s.id = li.sale_id").Where("s.business_id = ? AND s.created_at BETWEEN ? AND ?", id, from, to).Row().Scan(&out.UnitsSold, &costOfGoods); err != nil {
		return nil, err
	}
	out.Profit = out.Revenue - costOfGoods
	if out.SaleCount > 0 {
		out.AverageSale = out.Revenue / out.SaleCount
	}
	if err := r.db.Table("customers").Select("COALESCE(SUM(balance),0)").Where("business_id = ?", id).Scan(&out.CustomerCredit).Error; err != nil {
		return nil, err
	}
	if err := r.db.Table("suppliers").Select("COALESCE(SUM(outstanding_balance),0)").Where("business_id = ?", id).Scan(&out.SupplierDebt).Error; err != nil {
		return nil, err
	}
	if err := r.db.Table("stock_levels").Where("business_id = ? AND quantity <= low_stock_threshold", id).Count(&out.LowStockCount).Error; err != nil {
		return nil, err
	}
	return &out, nil
}
func (r *repository) TopProducts(id uuid.UUID, from, to time.Time, limit int) ([]ProductPerformance, error) {
	var out []ProductPerformance
	err := r.db.Table("sale_line_items li").Select("li.product_id, p.name product_name, SUM(li.quantity) quantity_sold, SUM(li.subtotal) revenue, SUM(li.subtotal - (li.quantity * li.unit_cost)) profit").Joins("JOIN sales s ON s.id = li.sale_id").Joins("JOIN products p ON p.id = li.product_id").Where("s.business_id = ? AND s.created_at BETWEEN ? AND ?", id, from, to).Group("li.product_id, p.name").Order("profit desc").Limit(limit).Scan(&out).Error
	return out, err
}
func (r *repository) SlowMoving(id uuid.UUID, cutoff time.Time) ([]SlowMovingProduct, error) {
	var out []SlowMovingProduct
	err := r.db.Table("products p").Select("p.id product_id, p.name product_name, COALESCE(sl.quantity,0) quantity_on_hand, MAX(s.created_at) last_sold_at, CASE WHEN MAX(s.created_at) IS NULL THEN NULL ELSE EXTRACT(DAY FROM NOW() - MAX(s.created_at))::bigint END days_since_sale").Joins("LEFT JOIN stock_levels sl ON sl.product_id = p.id AND sl.business_id = p.business_id").Joins("LEFT JOIN sale_line_items li ON li.product_id = p.id").Joins("LEFT JOIN sales s ON s.id = li.sale_id AND s.business_id = p.business_id").Where("p.business_id = ?", id).Group("p.id, p.name, sl.quantity").Having("MAX(s.created_at) IS NULL OR MAX(s.created_at) < ?", cutoff).Order("last_sold_at asc NULLS FIRST").Scan(&out).Error
	return out, err
}
