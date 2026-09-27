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

// NewService creates a new reports service with the given repository.
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// DailySalesReport returns daily sales summaries for the given business and date range.
// If From/To are not provided, defaults to the last 30 days.
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
