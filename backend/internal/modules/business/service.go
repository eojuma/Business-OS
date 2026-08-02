package business

import (
	"errors"

	"github.com/google/uuid"
)

var ErrBusinessNotFound = errors.New("business not found")

type CreateInput struct {
	Name     string
	Phone    string
	Email    string
	Address  string
	Currency string
}

type UpdateInput struct {
	Name    *string
	Phone   *string
	Email   *string
	Address *string
}

type Service interface {
	Create(input CreateInput) (*Business, error)
	Get(id uuid.UUID) (*Business, error)
	Update(id uuid.UUID, input UpdateInput) (*Business, error)
	SetOwner(businessID, ownerUserID uuid.UUID) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(input CreateInput) (*Business, error) {
	currency := input.Currency
	if currency == "" {
		currency = "KES"
	}

	b := &Business{
		Name:     input.Name,
		Phone:    input.Phone,
		Email:    input.Email,
		Address:  input.Address,
		Currency: currency,
	}

	if err := s.repo.Create(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *service) Get(id uuid.UUID) (*Business, error) {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return nil, ErrBusinessNotFound
	}
	return b, nil
}

func (s *service) Update(id uuid.UUID, input UpdateInput) (*Business, error) {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return nil, ErrBusinessNotFound
	}

	if input.Name != nil {
		b.Name = *input.Name
	}
	if input.Phone != nil {
		b.Phone = *input.Phone
	}
	if input.Email != nil {
		b.Email = *input.Email
	}
	if input.Address != nil {
		b.Address = *input.Address
	}

	if err := s.repo.Update(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *service) SetOwner(businessID, ownerUserID uuid.UUID) error {
	b, err := s.repo.FindByID(businessID)
	if err != nil {
		return ErrBusinessNotFound
	}
	b.OwnerUserID = &ownerUserID
	return s.repo.Update(b)
}
