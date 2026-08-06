package reports

import (
	"time"

	"github.com/google/uuid"
)

const defaultReportDays = 30

type DailySalesReportInput struct {
	BusinessID uuid.UUID
	From       *time.Time // nil = default to last 30 days
	To         *time.Time // nil = default to now
}

type Service interface {
	DailySalesReport(input DailySalesReportInput) ([]DailySalesSummary, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) DailySalesReport(input DailySalesReportInput) ([]DailySalesSummary, error) {
	to := time.Now()
	if input.To != nil {
		to = *input.To
	}

	from := to.AddDate(0, 0, -defaultReportDays)
	if input.From != nil {
		from = *input.From
	}

	return s.repo.DailySalesSummaries(input.BusinessID, from, to)
}