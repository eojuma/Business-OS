package products

import (
	"errors"

	"github.com/google/uuid"
)

var (
	ErrProductNotFound = errors.New("product not found")
	ErrInvalidPrice    = errors.New("price cannot be negative")
)

type CreateInput struct {
	BusinessID uuid.UUID
	Name       string
	Category   string
	Unit       string
	Price      int64 // cents
	SKU        string
}

type UpdateInput struct {
	Name     *string
	Category *string
	Unit     *string
	Price    *int64
	SKU      *string
}

type Service interface {
	Create(input CreateInput) (*Product, error)
	Get(id, businessID uuid.UUID) (*Product, error)
	List(businessID uuid.UUID) ([]Product, error)
	Update(id, businessID uuid.UUID, input UpdateInput) (*Product, error)
	Delete(id, businessID uuid.UUID) error
}

type service struct {
	repo Repository
}

// NewService creates a new product service with the given repository.
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// Create creates a new product with the given input.
func (s *service) Create(input CreateInput) (*Product, error) {
	if input.Price < 0 {
		return nil, ErrInvalidPrice
	}

	p := &Product{
		BusinessID: input.BusinessID,
		Name:       input.Name,
		Category:   input.Category,
		Unit:       input.Unit,
		Price:      input.Price,
		SKU:        input.SKU,
	}

	if err := s.repo.Create(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Get retrieves a product by ID for the given business.
func (s *service) Get(id, businessID uuid.UUID) (*Product, error) {
	p, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrProductNotFound
	}
	return p, nil
}

// List returns all products for the given business.
func (s *service) List(businessID uuid.UUID) ([]Product, error) {
	return s.repo.List(businessID)
}

// Update updates a product with the given input.
func (s *service) Update(id, businessID uuid.UUID, input UpdateInput) (*Product, error) {
	p, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrProductNotFound
	}

	if input.Price != nil {
		if *input.Price < 0 {
			return nil, ErrInvalidPrice
		}
		p.Price = *input.Price
	}
	if input.Name != nil {
		p.Name = *input.Name
	}
	if input.Category != nil {
		p.Category = *input.Category
	}
	if input.Unit != nil {
		p.Unit = *input.Unit
	}
	if input.SKU != nil {
		p.SKU = *input.SKU
	}

	if err := s.repo.Update(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Delete deletes a product by ID for the given business.
func (s *service) Delete(id, businessID uuid.UUID) error {
	if err := s.repo.Delete(id, businessID); err != nil {
		return err
	}
	return nil
}
