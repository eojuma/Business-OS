package reports

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)


type DailySalesSummary struct {
	Date         time.Time `json:"date"`
	TotalRevenue int64     `json:"total_revenue"` // cents
	SaleCount    int64     `json:"sale_count"`
}

type Repository interface {
	DailySalesSummaries(businessID uuid.UUID, from, to time.Time) ([]DailySalesSummary, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) DailySalesSummaries(businessID uuid.UUID, from, to time.Time) ([]DailySalesSummary, error) {
	var results []DailySalesSummary

	err := r.db.Table("sales").
		Select("DATE(created_at) as date, SUM(total_amount) as total_revenue, COUNT(*) as sale_count").
		Where("business_id = ? AND created_at BETWEEN ? AND ?", businessID, from, to).
		Group("DATE(created_at)").
		Order("date desc").
		Scan(&results).Error

	if err != nil {
		return nil, err
	}
	return results, nil
}