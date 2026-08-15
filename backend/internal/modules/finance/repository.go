package finance

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Summary struct {
	Revenue          int64 `json:"revenue"`
	CostOfGoods      int64 `json:"cost_of_goods"`
	GrossProfit      int64 `json:"gross_profit"`
	Expenses         int64 `json:"expenses"`
	NetProfit        int64 `json:"net_profit"`
	PurchasePayments int64 `json:"purchase_payments"`
	CashFlow         int64 `json:"cash_flow"`
}

type Repository interface {
	CreateExpense(*Expense) error
	ListExpenses(businessID uuid.UUID, from, to time.Time) ([]Expense, error)
	DeleteExpense(id, businessID uuid.UUID) error
	Summary(businessID uuid.UUID, from, to time.Time) (*Summary, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository           { return &repository{db: db} }
func (r *repository) CreateExpense(e *Expense) error { return r.db.Create(e).Error }
func (r *repository) ListExpenses(businessID uuid.UUID, from, to time.Time) ([]Expense, error) {
	var items []Expense
	err := r.db.Where("business_id = ? AND incurred_at BETWEEN ? AND ?", businessID, from, to).Order("incurred_at desc").Find(&items).Error
	return items, err
}
func (r *repository) DeleteExpense(id, businessID uuid.UUID) error {
	result := r.db.Where("id = ? AND business_id = ?", id, businessID).Delete(&Expense{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrExpenseNotFound
	}
	return nil
}
func (r *repository) Summary(businessID uuid.UUID, from, to time.Time) (*Summary, error) {
	var summary Summary
	if err := r.db.Table("sales").Select("COALESCE(SUM(total_amount), 0)").Where("business_id = ? AND created_at BETWEEN ? AND ?", businessID, from, to).Scan(&summary.Revenue).Error; err != nil {
		return nil, err
	}
	if err := r.db.Table("sale_line_items li").Select("COALESCE(SUM(li.quantity * li.unit_cost), 0)").Joins("JOIN sales s ON s.id = li.sale_id").Where("s.business_id = ? AND s.created_at BETWEEN ? AND ?", businessID, from, to).Scan(&summary.CostOfGoods).Error; err != nil {
		return nil, err
	}
	if err := r.db.Table("expenses").Select("COALESCE(SUM(amount), 0)").Where("business_id = ? AND incurred_at BETWEEN ? AND ?", businessID, from, to).Scan(&summary.Expenses).Error; err != nil {
		return nil, err
	}
	if err := r.db.Table("purchases").Select("COALESCE(SUM(amount_paid), 0)").Where("business_id = ? AND status = 'received' AND received_at BETWEEN ? AND ?", businessID, from, to).Scan(&summary.PurchasePayments).Error; err != nil {
		return nil, err
	}
	summary.GrossProfit = summary.Revenue - summary.CostOfGoods
	summary.NetProfit = summary.GrossProfit - summary.Expenses
	summary.CashFlow = summary.Revenue - summary.Expenses - summary.PurchasePayments
	return &summary, nil
}
