package audit

import (
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidDateRange is returned when the from date is after the to date.
var ErrInvalidDateRange = errors.New("from date must not be after to date")

// ErrInvalidPagination is returned when a page offset would overflow.
var ErrInvalidPagination = errors.New("page is too large")

// Service reads the audit trail.
type Service interface {
	List(businessID uuid.UUID, filter ListFilter) ([]Entry, int64, error)
}

type service struct{ repo Repository }

// NewService builds the audit reader service.
func NewService(repo Repository) Service { return &service{repo: repo} }

// List returns a normalized, paginated page of audit entries.
func (s *service) List(businessID uuid.UUID, filter ListFilter) ([]Entry, int64, error) {
	filter = normalizeFilter(filter)
	if filter.DateFrom != nil && filter.DateTo != nil && filter.DateFrom.After(*filter.DateTo) {
		return nil, 0, ErrInvalidDateRange
	}
	if filter.Page-1 > math.MaxInt/filter.Limit {
		return nil, 0, ErrInvalidPagination
	}
	return s.repo.List(businessID, filter)
}

// normalizeFilter applies page and limit bounds.
func normalizeFilter(filter ListFilter) ListFilter {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	filter.Action = strings.TrimSpace(filter.Action)
	filter.EntityType = strings.TrimSpace(filter.EntityType)
	return filter
}
