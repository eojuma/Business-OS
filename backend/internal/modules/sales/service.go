package sales

import (
	"errors"
	"log"

	"github.com/google/uuid"
)

var (
	ErrEmptySale           = errors.New("a sale must have at least one line item")
	ErrProductNotFound     = errors.New("one or more products not found")
	ErrInsufficientStock   = errors.New("insufficient stock for this sale")
	ErrCreditLimitExceeded = errors.New("this sale would exceed the customer's credit limit")
	ErrInvalidSaleItem     = errors.New("sale quantities must be positive")
	ErrInvalidDiscount     = errors.New("discount cannot be negative or exceed the sale subtotal")
	ErrInvalidSaleType     = errors.New("sale_type must be cash, credit or quotation")
	ErrCreditNeedsCustomer = errors.New("a credit sale requires a customer")
)

type ProductLookup interface {
	GetPricing(businessID, productID uuid.UUID) (price, costPrice int64, err error)
}

type SaleItemInput struct {
	ProductID uuid.UUID
	Quantity  int64
}

type CreateSaleInput struct {
	BusinessID uuid.UUID
	CustomerID *uuid.UUID
	SaleType   SaleType
	Discount   int64
	Note       string
	Items      []SaleItemInput
}

type Service interface {
	CreateSale(input CreateSaleInput) (*Sale, error)
	Get(id, businessID uuid.UUID) (*Sale, error)
	List(businessID uuid.UUID) ([]Sale, error)
}

// LowStockNotifier is the narrow hook sales needs from the notifications
// module. Satisfied by the notifications generator, wired in routes.go.
type LowStockNotifier interface {
	NotifyProductLowStock(businessID, productID uuid.UUID) error
}

type service struct {
	repo      Repository
	inventory InventoryMover
	products  ProductLookup
	customers CustomerCharger
	notifier  LowStockNotifier
}

func NewService(repo Repository, inventory InventoryMover, products ProductLookup, customers CustomerCharger, notifier LowStockNotifier) Service {
	return &service{repo: repo, inventory: inventory, products: products, customers: customers, notifier: notifier}
}

func (s *service) CreateSale(input CreateSaleInput) (*Sale, error) {
	if len(input.Items) == 0 {
		return nil, ErrEmptySale
	}

	saleType := input.SaleType
	if saleType == "" {
		saleType = SaleTypeCash
	}
	if !saleType.Valid() {
		return nil, ErrInvalidSaleType
	}
	if saleType == SaleTypeCredit && input.CustomerID == nil {
		return nil, ErrCreditNeedsCustomer
	}

	sale := &Sale{
		BusinessID: input.BusinessID,
		CustomerID: input.CustomerID,
		SaleType:   saleType,
		Discount:   input.Discount,
		Note:       input.Note,
	}

	lineItems := make([]SaleLineItem, 0, len(input.Items))
	var total int64

	for _, item := range input.Items {
		if item.Quantity <= 0 {
			return nil, ErrInvalidSaleItem
		}
		price, costPrice, err := s.products.GetPricing(input.BusinessID, item.ProductID)
		if err != nil {
			return nil, ErrProductNotFound
		}

		subtotal := price * item.Quantity
		total += subtotal

		lineItems = append(lineItems, SaleLineItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			UnitPrice: price,
			UnitCost:  costPrice,
			Subtotal:  subtotal,
		})
	}

	if input.Discount < 0 || input.Discount > total {
		return nil, ErrInvalidDiscount
	}
	sale.TotalAmount = total - input.Discount

	if err := s.repo.CreateSale(sale, lineItems, s.inventory, s.customers); err != nil {
		if errors.Is(err, ErrInsufficientStock) {
			return nil, ErrInsufficientStock
		}
		if errors.Is(err, ErrCreditLimitExceeded) {
			return nil, ErrCreditLimitExceeded
		}
		return nil, err
	}

	// Return the line items with the sale so the receipt can render them
	// without a second round trip.
	sale.LineItems = lineItems

	// A completed cash or credit sale may have pushed products to low stock.
	// Alert right away rather than waiting for the scheduled sweep. A
	// quotation moves no stock, so it is skipped. Best effort only.
	if s.notifier != nil && saleType != SaleTypeQuotation {
		for _, item := range input.Items {
			if nerr := s.notifier.NotifyProductLowStock(input.BusinessID, item.ProductID); nerr != nil {
				log.Printf("sales: low-stock notification failed: %v", nerr)
			}
		}
	}

	return sale, nil
}

func (s *service) Get(id, businessID uuid.UUID) (*Sale, error) {
	return s.repo.FindByID(id, businessID)
}

func (s *service) List(businessID uuid.UUID) ([]Sale, error) {
	return s.repo.List(businessID)
}
