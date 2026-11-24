package deliveries

import (
	"errors"
	"time"

	"github.com/businessos/backend/internal/modules/notifications"
	"github.com/google/uuid"
)

type Service interface {
	Create(businessID, actorID uuid.UUID, input CreateDeliveryInput) (*Delivery, error)
	Get(businessID, id uuid.UUID) (*Delivery, error)
	List(businessID uuid.UUID, sourceType string, sourceID *uuid.UUID, status string, limit, offset int) ([]Delivery, int64, error)
	Transition(businessID, actorID, id uuid.UUID, input TransitionInput) (*Delivery, error)
	Delete(businessID, id uuid.UUID) error
	History(businessID, id uuid.UUID) ([]DeliveryStatusHistory, error)
	DeliveryNote(businessID, id uuid.UUID) (map[string]any, error)
}

type service struct {
	repo  Repository
	notif notifications.Repository
}

func NewService(repo Repository, notif notifications.Repository) Service {
	return &service{repo: repo, notif: notif}
}

func (s *service) Create(businessID, actorID uuid.UUID, input CreateDeliveryInput) (*Delivery, error) {
	if input.SourceType != "sale" && input.SourceType != "purchase" {
		return nil, errors.New("invalid source_type")
	}
	sid, err := uuid.Parse(input.SourceID)
	if err != nil {
		return nil, errors.New("invalid source_id")
	}
	d := &Delivery{
		BusinessID:     businessID,
		SourceType:     input.SourceType,
		SourceID:       sid,
		Status:         StatusPending,
		TrackingNumber: input.TrackingNumber,
		Carrier:        input.Carrier,
		Notes:          input.Notes,
		CreatedBy:      &actorID,
	}
	for _, it := range input.Items {
		pid, err := uuid.Parse(it.ProductID)
		if err != nil {
			return nil, errors.New("invalid product_id")
		}
		if it.Quantity <= 0 {
			return nil, errors.New("quantity must be positive")
		}
		d.Items = append(d.Items, DeliveryItem{ProductID: pid, Quantity: it.Quantity})
	}
	if err := s.repo.Create(d); err != nil {
		return nil, err
	}
	// record initial history
	_ = s.repo.CreateHistory(&DeliveryStatusHistory{
		DeliveryID:  d.ID,
		BusinessID:  businessID,
		FromStatus:  "",
		ToStatus:    StatusPending,
		ActorUserID: &actorID,
		Note:        "created",
	})
	return d, nil
}

func (s *service) Get(businessID, id uuid.UUID) (*Delivery, error) {
	return s.repo.GetByID(id, businessID)
}

func (s *service) List(businessID uuid.UUID, sourceType string, sourceID *uuid.UUID, status string, limit, offset int) ([]Delivery, int64, error) {
	return s.repo.List(businessID, sourceType, sourceID, status, limit, offset)
}

func (s *service) Transition(businessID, actorID, id uuid.UUID, input TransitionInput) (*Delivery, error) {
	to := DeliveryStatus(input.Status)
	if !to.Valid() {
		return nil, errors.New("invalid status")
	}
	d, err := s.repo.GetByID(id, businessID)
	if err != nil {
		return nil, err
	}
	if d.Status.IsTerminal() && to != StatusFailed {
		// delivered is terminal
	}
	if !CanTransition(d.Status, to) {
		return nil, errors.New("invalid status transition")
	}
	now := time.Now()
	from := d.Status
	switch to {
	case StatusDispatched:
		if d.DispatchedAt == nil {
			d.DispatchedAt = &now
		}
	case StatusInTransit:
		if d.InTransitAt == nil {
			d.InTransitAt = &now
		}
	case StatusDelivered:
		if d.DeliveredAt == nil {
			d.DeliveredAt = &now
		}
	case StatusFailed:
		if d.FailedAt == nil {
			d.FailedAt = &now
		}
		d.FailureReason = input.Note
	}
	d.Status = to
	if err := s.repo.Update(d); err != nil {
		return nil, err
	}
	_ = s.repo.CreateHistory(&DeliveryStatusHistory{
		DeliveryID:  d.ID,
		BusinessID:  businessID,
		FromStatus:  from,
		ToStatus:    to,
		ActorUserID: &actorID,
		Note:        input.Note,
	})
	// notification hook
	s.notify(businessID, d, to)
	return d, nil
}

func (s *service) notify(businessID uuid.UUID, d *Delivery, to DeliveryStatus) {
	if s.notif == nil {
		return
	}
	if to != StatusDispatched && to != StatusDelivered && to != StatusFailed {
		return
	}
	severity := notifications.SeverityInfo
	title := "Delivery update"
	msg := "Delivery " + d.ID.String() + " moved to " + string(to)
	if to == StatusFailed {
		severity = notifications.SeverityWarning
		title = "Delivery failed"
		msg = "Delivery " + d.ID.String() + " failed: " + d.FailureReason
	}
	n := &notifications.Notification{
		BusinessID: businessID,
		Type:       "delivery",
		Severity:   severity,
		Title:      title,
		Message:    msg,
		EntityID:   &d.ID,
		EntityName: d.SourceType + " " + d.SourceID.String(),
	}
	_ = s.notif.Create(n)
}

func (s *service) Delete(businessID, id uuid.UUID) error {
	return s.repo.Delete(id, businessID)
}

func (s *service) History(businessID, id uuid.UUID) ([]DeliveryStatusHistory, error) {
	d, err := s.repo.GetByID(id, businessID)
	if err != nil {
		return nil, err
	}
	_ = d
	return s.repo.ListHistory(id)
}

func (s *service) DeliveryNote(businessID, id uuid.UUID) (map[string]any, error) {
	d, err := s.repo.GetByID(id, businessID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"delivery_id":     d.ID,
		"source_type":     d.SourceType,
		"source_id":       d.SourceID,
		"status":          d.Status,
		"tracking_number": d.TrackingNumber,
		"carrier":         d.Carrier,
		"notes":           d.Notes,
		"items":           d.Items,
		"dispatched_at":   d.DispatchedAt,
		"delivered_at":    d.DeliveredAt,
		"created_at":      d.CreatedAt,
	}, nil
}
