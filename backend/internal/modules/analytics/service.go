package analytics

import (
	"errors"
	"github.com/google/uuid"
	"time"
)

var ErrInvalidRange = errors.New("from date must not be after to date")

type Service interface {
	Overview(uuid.UUID, time.Time, time.Time) (*Overview, error)
	TopProducts(uuid.UUID, time.Time, time.Time, int) ([]ProductPerformance, error)
	SlowMoving(uuid.UUID, int) ([]SlowMovingProduct, error)
}
type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }
func (s *service) Overview(id uuid.UUID, from, to time.Time) (*Overview, error) {
	if from.After(to) {
		return nil, ErrInvalidRange
	}
	return s.repo.Overview(id, from, to)
}
func (s *service) TopProducts(id uuid.UUID, from, to time.Time, limit int) ([]ProductPerformance, error) {
	if from.After(to) {
		return nil, ErrInvalidRange
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	return s.repo.TopProducts(id, from, to, limit)
}
func (s *service) SlowMoving(id uuid.UUID, days int) ([]SlowMovingProduct, error) {
	if days < 1 {
		days = 60
	}
	return s.repo.SlowMoving(id, time.Now().AddDate(0, 0, -days))
}
