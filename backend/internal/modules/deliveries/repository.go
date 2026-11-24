package deliveries

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrDeliveryNotFound = errors.New("delivery not found")
)

type Repository interface {
	Create(d *Delivery) error
	GetByID(id, businessID uuid.UUID) (*Delivery, error)
	List(businessID uuid.UUID, sourceType string, sourceID *uuid.UUID, status string, limit, offset int) ([]Delivery, int64, error)
	Update(d *Delivery) error
	Delete(id, businessID uuid.UUID) error
	CreateHistory(h *DeliveryStatusHistory) error
	ListHistory(deliveryID uuid.UUID) ([]DeliveryStatusHistory, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(d *Delivery) error {
	return r.db.Create(d).Error
}

func (r *repository) GetByID(id, businessID uuid.UUID) (*Delivery, error) {
	var d Delivery
	err := r.db.Preload("Items").Where("id = ? AND business_id = ?", id, businessID).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (r *repository) List(businessID uuid.UUID, sourceType string, sourceID *uuid.UUID, status string, limit, offset int) ([]Delivery, int64, error) {
	var items []Delivery
	q := r.db.Where("business_id = ?", businessID)
	if sourceType != "" {
		q = q.Where("source_type = ?", sourceType)
	}
	if sourceID != nil {
		q = q.Where("source_id = ?", *sourceID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Model(&Delivery{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	err := q.Preload("Items").Order("created_at desc").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *repository) Update(d *Delivery) error {
	return r.db.Save(d).Error
}

func (r *repository) Delete(id, businessID uuid.UUID) error {
	res := r.db.Where("id = ? AND business_id = ?", id, businessID).Delete(&Delivery{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (r *repository) CreateHistory(h *DeliveryStatusHistory) error {
	return r.db.Create(h).Error
}

func (r *repository) ListHistory(deliveryID uuid.UUID) ([]DeliveryStatusHistory, error) {
	var items []DeliveryStatusHistory
	err := r.db.Where("delivery_id = ?", deliveryID).Order("created_at asc").Find(&items).Error
	return items, err
}
