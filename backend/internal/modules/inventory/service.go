package inventory

import (
	"errors"
	"log"

	"github.com/google/uuid"
)

var (
	ErrInsufficientStock = errors.New("insufficient stock for this movement")
	ErrProductNotFound   = errors.New("product not found in inventory")
)

// LowStockNotifier is the narrow hook inventory needs from the notifications
// module. It is satisfied by the notifications generator and wired in
// routes.go, so inventory never imports notifications directly.
type LowStockNotifier interface {
	NotifyProductLowStock(businessID, productID uuid.UUID) error
}

type RecordMovementInput struct {
	BusinessID uuid.UUID
	ProductID  uuid.UUID
	Type       MovementType
	Direction  MovementDirection
	Quantity   int64
	Note       string
}

type Service interface {
	RecordMovement(input RecordMovementInput) (*StockLevel, error)
	GetStockLevel(productID, businessID uuid.UUID) (*StockLevel, error)
	ListLowStock(businessID uuid.UUID) ([]StockLevel, error)
	ListMovements(productID, businessID uuid.UUID) ([]StockMovement, error)
}

type service struct {
	repo     Repository
	notifier LowStockNotifier
}

func NewService(repo Repository, notifier LowStockNotifier) Service {
	return &service{repo: repo, notifier: notifier}
}

func (s *service) RecordMovement(input RecordMovementInput) (*StockLevel, error) {
	direction := input.Direction
	if direction != DirectionOut {
		direction = DirectionIn
	}

	// Pre-check so the caller gets a clean error before we open a transaction.
	// recordMovementCore repeats the check as the authoritative guard.
	signed := SignedQuantity(input.Type, direction, input.Quantity)
	if signed < 0 {
		current, err := s.repo.GetStockLevel(input.ProductID, input.BusinessID)
		currentQty := int64(0)
		if err == nil {
			currentQty = current.Quantity
		}
		if currentQty+signed < 0 {
			return nil, ErrInsufficientStock
		}
	}

	movement := &StockMovement{
		BusinessID: input.BusinessID,
		ProductID:  input.ProductID,
		Type:       input.Type,
		Quantity:   input.Quantity,
		Note:       input.Note,
	}

	level, err := s.repo.RecordMovement(movement, direction)
	if err != nil {
		return nil, err
	}

	// Fire the alert immediately so the notifications page updates without
	// waiting for the scheduled sweep. Best effort: a notification failure
	// must never roll back a stock movement that already committed.
	if s.notifier != nil {
		if nerr := s.notifier.NotifyProductLowStock(input.BusinessID, input.ProductID); nerr != nil {
			log.Printf("inventory: low-stock notification failed: %v", nerr)
		}
	}

	return level, nil
}

func (s *service) GetStockLevel(productID, businessID uuid.UUID) (*StockLevel, error) {
	level, err := s.repo.GetStockLevel(productID, businessID)
	if err != nil {
		return nil, ErrProductNotFound
	}
	return level, nil
}

func (s *service) ListLowStock(businessID uuid.UUID) ([]StockLevel, error) {
	return s.repo.ListLowStock(businessID)
}

func (s *service) ListMovements(productID, businessID uuid.UUID) ([]StockMovement, error) {
	return s.repo.ListMovements(productID, businessID)
}
