package sales

import (
	"errors"

	"github.com/google/uuid"
)

var (
	ErrEmptySale           = errors.New("a sale must have at least one line item")
	ErrProductNotFound     = errors.New("one or more products not found")
	ErrInsufficientStock   = errors.New("insufficient stock for this sale")
	ErrCreditLimitExceeded = errors.New("this sale would exceed the customer's credit limit")
	ErrInvalidSaleItem     = errors.New("sale quantities must be positive")
	ErrInvalidDiscount     = errors.New("discount cannot be negative or exceed the sale subtotal")
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
	Discount   int64
	Note       string
	Items      []SaleItemInput
}

type Service interface {
	CreateSale(input CreateSaleInput) (*Sale, error)
	Get(id, businessID uuid.UUID) (*Sale, error)
	List(businessID uuid.UUID) ([]Sale, error)
}

type service struct {
	repo      Repository
	inventory InventoryMover
	products  ProductLookup
	customers CustomerCharger
}

func NewService(repo Repository, inventory InventoryMover, products ProductLookup, customers CustomerCharger) Service {
	return &service{repo: repo, inventory: inventory, products: products, customers: customers}
}

func (s *service) CreateSale(input CreateSaleInput) (*Sale, error) {
	if len(input.Items) == 0 {
		return nil, ErrEmptySale
	}

	sale := &Sale{
		BusinessID: input.BusinessID,
		CustomerID: input.CustomerID,
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

	return sale, nil
}

func (s *service) Get(id, businessID uuid.UUID) (*Sale, error) {
	return s.repo.FindByID(id, businessID)
}

func (s *service) List(businessID uuid.UUID) ([]Sale, error) {
	return s.repo.List(businessID)
}
