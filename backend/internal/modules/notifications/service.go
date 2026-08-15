package notifications

import "github.com/google/uuid"

type Service interface {
	List(uuid.UUID) ([]Notification, error)
}
type service struct{ repo Repository }

func NewService(repo Repository) Service                     { return &service{repo: repo} }
func (s *service) List(id uuid.UUID) ([]Notification, error) { return s.repo.LowStock(id) }
