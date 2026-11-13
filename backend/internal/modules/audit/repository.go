package audit

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ListFilter narrows the audit viewer query.
type ListFilter struct {
	Page       int
	Limit      int
	ActorID    *uuid.UUID
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	DateFrom   *time.Time
	DateTo     *time.Time
}

// Repository stores and reads append-only audit entries.
type Repository interface {
	Create(entry *Entry) error
	List(businessID uuid.UUID, filter ListFilter) ([]Entry, int64, error)
}

type repository struct{ db *gorm.DB }

// NewRepository builds an audit repository over the given database.
func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

// Create appends one audit entry.
func (r *repository) Create(entry *Entry) error {
	return r.db.Create(entry).Error
}

// List returns filtered audit entries with pagination.
func (r *repository) List(businessID uuid.UUID, filter ListFilter) ([]Entry, int64, error) {
	query := r.db.Model(&Entry{}).Where("business_id = ?", businessID)
	if filter.ActorID != nil {
		query = query.Where("actor_user_id = ?", *filter.ActorID)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.EntityType != "" {
		query = query.Where("entity_type = ?", filter.EntityType)
	}
	if filter.EntityID != nil {
		query = query.Where("entity_id = ?", *filter.EntityID)
	}
	if filter.DateFrom != nil {
		query = query.Where("created_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query = query.Where("created_at <= ?", *filter.DateTo)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entries []Entry
	err := query.
		Order("created_at desc, id desc").
		Offset((filter.Page - 1) * filter.Limit).
		Limit(filter.Limit).
		Find(&entries).Error
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
