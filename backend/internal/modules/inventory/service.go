package inventory

import (
	"errors"

	"github.com/google/uuid"
)

var (
	ErrInsufficientStock = errors.New("insufficient stock for this movement")
	ErrProductNotFound   = errors.New("product not found in inventory")
)

type RecordMovementInput struct {
	BusinessID uuid.UUID
	ProductID  uuid.UUID
	Type       MovementType
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
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func isOutgoing(t MovementType) bool {
	return t == MovementSale
}

func (s *service) RecordMovement(input RecordMovementInput) (*StockLevel, error) {
	quantity := input.Quantity
	if isOutgoing(input.Type) {
		quantity = -quantity

		current, err := s.repo.GetStockLevel(input.ProductID, input.BusinessID)
		currentQty := int64(0)
		if err == nil {
			currentQty = current.Quantity
		}
		if currentQty+quantity < 0 {
			return nil, ErrInsufficientStock
		}
	}

	movement := &StockMovement{
		BusinessID: input.BusinessID,
		ProductID:  input.ProductID,
		Type:       input.Type,
		Quantity:   quantity,
		Note:       input.Note,
	}

	return s.repo.RecordMovement(movement)
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