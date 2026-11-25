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
	CreateDeliveryWithHistory(d *Delivery, h *DeliveryStatusHistory) error
	GetByID(id, businessID uuid.UUID) (*Delivery, error)
	List(businessID uuid.UUID, sourceType string, sourceID *uuid.UUID, status string, limit, offset int) ([]Delivery, int64, error)
	Update(d *Delivery) error
	TransitionDelivery(d *Delivery, from DeliveryStatus, h *DeliveryStatusHistory) (bool, error)
	Delete(id, businessID uuid.UUID) error
	SaleExists(businessID, id uuid.UUID) (bool, error)
	PurchaseExists(businessID, id uuid.UUID) (bool, error)
	ProductExists(businessID, id uuid.UUID) (bool, error)
	CreateHistory(h *DeliveryStatusHistory) error
	ListHistory(deliveryID uuid.UUID) ([]DeliveryStatusHistory, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(d *Delivery) error {
	return r.db.Create(d).Error
}

func (r *repository) CreateDeliveryWithHistory(d *Delivery, h *DeliveryStatusHistory) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(d).Error; err != nil {
			return err
		}
		if h != nil {
			h.DeliveryID = d.ID
			if err := tx.Create(h).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *repository) TransitionDelivery(d *Delivery, from DeliveryStatus, h *DeliveryStatusHistory) (bool, error) {
	var affected int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Delivery{}).
			Where("id = ? AND business_id = ? AND status = ?", d.ID, d.BusinessID, from).
			Updates(map[string]any{
				"status":          d.Status,
				"tracking_number": d.TrackingNumber,
				"carrier":         d.Carrier,
				"notes":           d.Notes,
				"failure_reason":  d.FailureReason,
				"dispatched_at":   d.DispatchedAt,
				"in_transit_at":   d.InTransitAt,
				"delivered_at":    d.DeliveredAt,
				"failed_at":       d.FailedAt,
				"updated_at":      d.UpdatedAt,
			})
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		if affected == 0 {
			return nil
		}
		if h != nil {
			if err := tx.Create(h).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return affected > 0, nil
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

func (r *repository) SaleExists(businessID, id uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Table("sales").Where("id = ? AND business_id = ?", id, businessID).Count(&count).Error
	return count > 0, err
}

func (r *repository) PurchaseExists(businessID, id uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Table("purchases").Where("id = ? AND business_id = ?", id, businessID).Count(&count).Error
	return count > 0, err
}

func (r *repository) ProductExists(businessID, id uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Table("products").Where("id = ? AND business_id = ?", id, businessID).Count(&count).Error
	return count > 0, err
}

func (r *repository) CreateHistory(h *DeliveryStatusHistory) error {
	return r.db.Create(h).Error
}

func (r *repository) ListHistory(deliveryID uuid.UUID) ([]DeliveryStatusHistory, error) {
	var items []DeliveryStatusHistory
	err := r.db.Where("delivery_id = ?", deliveryID).Order("created_at asc").Find(&items).Error
	return items, err
}
