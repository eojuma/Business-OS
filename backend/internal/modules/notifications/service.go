package notifications

import "github.com/google/uuid"

type Service interface {
	List(businessID uuid.UUID, unreadOnly bool) ([]Notification, error)
	UnreadCount(businessID uuid.UUID) (int64, error)
	MarkRead(id, businessID uuid.UUID) error
	MarkAllRead(businessID uuid.UUID) error
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

func (s *service) List(businessID uuid.UUID, unreadOnly bool) ([]Notification, error) {
	return s.repo.List(businessID, unreadOnly)
}

func (s *service) UnreadCount(businessID uuid.UUID) (int64, error) {
	return s.repo.UnreadCount(businessID)
}

func (s *service) MarkRead(id, businessID uuid.UUID) error {
	return s.repo.MarkRead(id, businessID)
}

func (s *service) MarkAllRead(businessID uuid.UUID) error {
	return s.repo.MarkAllRead(businessID)
}
