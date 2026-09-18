package notifications

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("notification not found")

type Repository interface {
	List(businessID uuid.UUID, unreadOnly bool) ([]Notification, error)
	UnreadCount(businessID uuid.UUID) (int64, error)
	MarkRead(id, businessID uuid.UUID) error
	MarkAllRead(businessID uuid.UUID) error
	Create(n *Notification) error
	ExistsUnread(businessID uuid.UUID, notificationType string, entityID uuid.UUID) (bool, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) List(businessID uuid.UUID, unreadOnly bool) ([]Notification, error) {
	var items []Notification
	q := r.db.Where("business_id = ?", businessID)
	if unreadOnly {
		q = q.Where("is_read = ?", false)
	}
	err := q.Order("created_at desc").Find(&items).Error
	return items, err
}

func (r *repository) UnreadCount(businessID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&Notification{}).
		Where("business_id = ? AND is_read = ?", businessID, false).
		Count(&count).Error
	return count, err
}

func (r *repository) MarkRead(id, businessID uuid.UUID) error {
	res := r.db.Model(&Notification{}).
		Where("id = ? AND business_id = ?", id, businessID).
		Update("is_read", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) MarkAllRead(businessID uuid.UUID) error {
	return r.db.Model(&Notification{}).
		Where("business_id = ? AND is_read = ?", businessID, false).
		Update("is_read", true).Error
}

func (r *repository) Create(n *Notification) error {
	return r.db.Create(n).Error
}

func (r *repository) ExistsUnread(businessID uuid.UUID, notificationType string, entityID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&Notification{}).
		Where("business_id = ? AND type = ? AND entity_id = ? AND is_read = ?", businessID, notificationType, entityID, false).
		Count(&count).Error
	return count > 0, err
}
