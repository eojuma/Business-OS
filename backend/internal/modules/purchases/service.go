package purchases

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrPurchaseNotFound       = errors.New("purchase not found")
	ErrEmptyPurchase          = errors.New("a purchase must have at least one line item")
	ErrInvalidPurchaseItem    = errors.New("purchase quantities must be positive and costs cannot be negative")
	ErrInvalidAmountPaid      = errors.New("amount paid cannot be negative or exceed the purchase total")
	ErrAlreadyReceived        = errors.New("purchase has already been received")
	ErrInvalidReceiveQuantity = errors.New("received quantity must be positive and not exceed the remaining quantity")
	ErrUnknownReceiveItem     = errors.New("purchase does not contain the requested product")
	ErrDuplicateReceiveItem   = errors.New("duplicate product in receive request")
	ErrNothingToReceive       = errors.New("purchase has no remaining items to receive")
)

type ItemInput struct {
	ProductID          uuid.UUID
	Quantity, UnitCost int64
	Note               string
}
type CreateInput struct {
	BusinessID, SupplierID uuid.UUID
	AmountPaid             int64
	Note                   string
	ExpectedAt             *time.Time
	Items                  []ItemInput
}

type ReceiveItemInput struct {
	ProductID   uuid.UUID
	ReceivedQty int64
}
type ReceiveInput struct {
	Items []ReceiveItemInput
}

type Service interface {
	Create(CreateInput) (*Purchase, error)
	Get(id, businessID uuid.UUID) (*Purchase, error)
	List(businessID uuid.UUID, supplierID *uuid.UUID) ([]Purchase, error)
	Receive(id, businessID uuid.UUID, input ReceiveInput) (*Purchase, error)
	ListPaginated(businessID uuid.UUID, page, limit int, status *string, supplierID *uuid.UUID, dateFrom *string, dateTo *string, poNumber *string) ([]Purchase, int64, error)
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
	// Copy the optional expected date and per-line notes onto the persisted
	// purchase model; the repository stores them as-is.
	items := make([]PurchaseLineItem, 0, len(input.Items))
	var total int64
	for _, item := range input.Items {
		if item.Quantity <= 0 || item.UnitCost < 0 {
			return nil, ErrInvalidPurchaseItem
		}
		subtotal := item.Quantity * item.UnitCost
		total += subtotal
		items = append(items, PurchaseLineItem{ProductID: item.ProductID, Quantity: item.Quantity, UnitCost: item.UnitCost, Subtotal: subtotal, Note: item.Note})
	}
	if input.AmountPaid < 0 || input.AmountPaid > total {
		return nil, ErrInvalidAmountPaid
	}
	purchase := &Purchase{BusinessID: input.BusinessID, SupplierID: input.SupplierID, Status: "draft", TotalAmount: total, AmountPaid: input.AmountPaid, Note: input.Note, ExpectedAt: input.ExpectedAt}
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

func (s *service) ListPaginated(businessID uuid.UUID, page, limit int, status *string, supplierID *uuid.UUID, dateFrom *string, dateTo *string, poNumber *string) ([]Purchase, int64, error) {
	return s.repo.ListPaginated(businessID, page, limit, status, supplierID, dateFrom, dateTo, poNumber)
}
func (s *service) Receive(id, businessID uuid.UUID, input ReceiveInput) (*Purchase, error) {
	p, err := s.repo.Receive(id, businessID, input.Items, s.inventory, s.suppliers, s.products)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPurchaseNotFound
	}
	return p, err
}
