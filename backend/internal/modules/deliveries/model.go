package deliveries

import (
	"time"

	"github.com/google/uuid"
)

// DeliveryStatus is the lifecycle state of a delivery.
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusDispatched DeliveryStatus = "dispatched"
	StatusInTransit  DeliveryStatus = "in_transit"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusFailed     DeliveryStatus = "failed"
)

func (s DeliveryStatus) Valid() bool {
	switch s {
	case StatusPending, StatusDispatched, StatusInTransit, StatusDelivered, StatusFailed:
		return true
	}
	return false
}

// IsTerminal reports whether no further transitions are allowed.
func (s DeliveryStatus) IsTerminal() bool {
	return s == StatusDelivered
}

// Delivery links a sale or purchase to a physical shipment.
type Delivery struct {
	ID             uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID     uuid.UUID      `gorm:"type:uuid;index;not null" json:"business_id"`
	SourceType     string         `gorm:"not null" json:"source_type"` // "sale" or "purchase"
	SourceID       uuid.UUID      `gorm:"type:uuid;index;not null" json:"source_id"`
	Status         DeliveryStatus `gorm:"not null;default:'pending'" json:"status"`
	TrackingNumber string         `gorm:"not null;default:''" json:"tracking_number"`
	Carrier        string         `gorm:"not null;default:''" json:"carrier"`
	Notes          string         `gorm:"not null;default:''" json:"notes"`
	FailureReason  string         `gorm:"not null;default:''" json:"failure_reason"`
	DispatchedAt   *time.Time     `gorm:"type:timestamptz" json:"dispatched_at,omitempty"`
	InTransitAt    *time.Time     `gorm:"type:timestamptz" json:"in_transit_at,omitempty"`
	DeliveredAt    *time.Time     `gorm:"type:timestamptz" json:"delivered_at,omitempty"`
	FailedAt       *time.Time     `gorm:"type:timestamptz" json:"failed_at,omitempty"`
	CreatedBy      *uuid.UUID     `gorm:"type:uuid" json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`

	Items []DeliveryItem `gorm:"foreignKey:DeliveryID" json:"items,omitempty"`
}

func (Delivery) TableName() string { return "deliveries" }

type DeliveryItem struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	DeliveryID uuid.UUID `gorm:"type:uuid;index;not null" json:"delivery_id"`
	ProductID  uuid.UUID `gorm:"type:uuid;index;not null" json:"product_id"`
	Quantity   int64     `gorm:"not null" json:"quantity"`
	CreatedAt  time.Time `json:"created_at"`
}

func (DeliveryItem) TableName() string { return "delivery_items" }

type DeliveryStatusHistory struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	DeliveryID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"delivery_id"`
	BusinessID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"business_id"`
	FromStatus  DeliveryStatus `gorm:"not null;default:''" json:"from_status"`
	ToStatus    DeliveryStatus `gorm:"not null" json:"to_status"`
	ActorUserID *uuid.UUID     `gorm:"type:uuid" json:"actor_user_id,omitempty"`
	Note        string         `gorm:"not null;default:''" json:"note"`
	CreatedAt   time.Time      `json:"created_at"`
}

func (DeliveryStatusHistory) TableName() string { return "delivery_status_history" }

// CreateDeliveryInput is the request body for creating a delivery.
type CreateDeliveryInput struct {
	SourceType     string `json:"source_type" binding:"required"` // sale or purchase
	SourceID       string `json:"source_id" binding:"required,uuid"`
	TrackingNumber string `json:"tracking_number"`
	Carrier        string `json:"carrier"`
	Notes          string `json:"notes"`
	Items          []struct {
		ProductID string `json:"product_id" binding:"required,uuid"`
		Quantity  int64  `json:"quantity" binding:"required,gt=0"`
	} `json:"items" binding:"required,min=1"`
}

// TransitionInput is the request body for a status change.
type TransitionInput struct {
	Status string `json:"status" binding:"required"`
	Note   string `json:"note"`
}

// AllowedTransitions defines the status machine.
var AllowedTransitions = map[DeliveryStatus][]DeliveryStatus{
	StatusPending:    {StatusDispatched},
	StatusDispatched: {StatusInTransit, StatusDelivered, StatusFailed},
	StatusInTransit:  {StatusDelivered, StatusFailed},
	StatusFailed:     {StatusDispatched},
	StatusDelivered:  {},
}

func CanTransition(from, to DeliveryStatus) bool {
	for _, s := range AllowedTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}
