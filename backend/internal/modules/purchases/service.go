package purchases

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrPurchaseNotFound    = errors.New("purchase not found")
	ErrEmptyPurchase       = errors.New("a purchase must have at least one line item")
	ErrInvalidPurchaseItem = errors.New("purchase quantities must be positive and costs cannot be negative")
	ErrInvalidAmountPaid   = errors.New("amount paid cannot be negative or exceed the purchase total")
	ErrAlreadyReceived     = errors.New("purchase has already been received")
)

type ItemInput struct {
	ProductID          uuid.UUID
	Quantity, UnitCost int64
}
type CreateInput struct {
	BusinessID, SupplierID uuid.UUID
	AmountPaid             int64
	Note                   string
	Items                  []ItemInput
}

type Service interface {
	Create(CreateInput) (*Purchase, error)
	Get(id, businessID uuid.UUID) (*Purchase, error)
	List(businessID uuid.UUID, supplierID *uuid.UUID) ([]Purchase, error)
	Receive(id, businessID uuid.UUID) (*Purchase, error)
}

type service struct {
	repo      Repository
	inventory InventoryReceiver
	suppliers SupplierCharger
	products  ProductCostUpdater
}

func NewService(repo Repository, inventory InventoryReceiver, suppliers SupplierCharger, products ProductCostUpdater) Service {
	return &service{repo: repo, inventory: inventory, suppliers: suppliers, products: products}
}

func (s *service) Create(input CreateInput) (*Purchase, error) {
	if len(input.Items) == 0 {
		return nil, ErrEmptyPurchase
	}
	items := make([]PurchaseLineItem, 0, len(input.Items))
	var total int64
	for _, item := range input.Items {
		if item.Quantity <= 0 || item.UnitCost < 0 {
			return nil, ErrInvalidPurchaseItem
		}
		subtotal := item.Quantity * item.UnitCost
		total += subtotal
		items = append(items, PurchaseLineItem{ProductID: item.ProductID, Quantity: item.Quantity, UnitCost: item.UnitCost, Subtotal: subtotal})
	}
	if input.AmountPaid < 0 || input.AmountPaid > total {
		return nil, ErrInvalidAmountPaid
	}
	purchase := &Purchase{BusinessID: input.BusinessID, SupplierID: input.SupplierID, Status: "draft", TotalAmount: total, AmountPaid: input.AmountPaid, Note: input.Note}
	if err := s.repo.Create(purchase, items); err != nil {
		return nil, err
	}
	return purchase, nil
}

func (s *service) Get(id, businessID uuid.UUID) (*Purchase, error) {
	p, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrPurchaseNotFound
	}
	return p, nil
}
func (s *service) List(businessID uuid.UUID, supplierID *uuid.UUID) ([]Purchase, error) {
	return s.repo.List(businessID, supplierID)
}
func (s *service) Receive(id, businessID uuid.UUID) (*Purchase, error) {
	p, err := s.repo.Receive(id, businessID, s.inventory, s.suppliers, s.products)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPurchaseNotFound
	}
	return p, err
}
