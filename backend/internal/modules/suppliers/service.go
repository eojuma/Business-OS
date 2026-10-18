package suppliers

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrSupplierNotFound      = errors.New("supplier not found")
	ErrInvalidPayment        = errors.New("payment amount must be positive")
	ErrPaymentExceedsBalance = errors.New("payment exceeds supplier outstanding balance")
	ErrInvalidPaymentTerms   = errors.New("invalid payment terms")
	ErrSupplierHasPurchases  = errors.New("supplier has purchases and cannot be archived")
)

type CreateInput struct {
	BusinessID                  uuid.UUID
	Name, Phone, Email, Address string
	PaymentTerms                string
}

type UpdateInput struct {
	Name, Phone, Email, Address *string
	PaymentTerms                *string
}

type Service interface {
	Create(CreateInput) (*Supplier, error)
	Get(id, businessID uuid.UUID) (*Supplier, error)
	List(businessID uuid.UUID, includeArchived bool) ([]Supplier, error)
	Update(id, businessID uuid.UUID, input UpdateInput) (*Supplier, error)
	Delete(id, businessID uuid.UUID) error
	Restore(id, businessID uuid.UUID) (*Supplier, error)
	RecordPayment(id, businessID uuid.UUID, amount int64, note string) (*Supplier, error)
	ListPayments(id, businessID uuid.UUID) ([]Payment, error)
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

func (s *service) Create(input CreateInput) (*Supplier, error) {
	terms := input.PaymentTerms
	if terms == "" {
		terms = PaymentTermsDefault
	}
	if !IsValidPaymentTerms(terms) {
		return nil, ErrInvalidPaymentTerms
	}
	supplier := &Supplier{BusinessID: input.BusinessID, Name: input.Name, Phone: input.Phone, Email: input.Email, Address: input.Address, PaymentTerms: terms}
	return supplier, s.repo.Create(supplier)
}

func (s *service) Get(id, businessID uuid.UUID) (*Supplier, error) {
	supplier, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrSupplierNotFound
	}
	return supplier, nil
}

func (s *service) List(businessID uuid.UUID, includeArchived bool) ([]Supplier, error) {
	return s.repo.List(businessID, includeArchived)
}

func (s *service) Update(id, businessID uuid.UUID, input UpdateInput) (*Supplier, error) {
	supplier, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrSupplierNotFound
	}
	if input.Name != nil {
		supplier.Name = *input.Name
	}
	if input.Phone != nil {
		supplier.Phone = *input.Phone
	}
	if input.Email != nil {
		supplier.Email = *input.Email
	}
	if input.Address != nil {
		supplier.Address = *input.Address
	}
	if input.PaymentTerms != nil {
		if !IsValidPaymentTerms(*input.PaymentTerms) {
			return nil, ErrInvalidPaymentTerms
		}
		supplier.PaymentTerms = *input.PaymentTerms
	}
	return supplier, s.repo.Update(supplier)
}

func (s *service) Delete(id, businessID uuid.UUID) error {
	err := s.repo.Delete(id, businessID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSupplierNotFound
	}
	return err
}

func (s *service) Restore(id, businessID uuid.UUID) (*Supplier, error) {
	if err := s.repo.Restore(id, businessID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrSupplierNotFound) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}
	return s.Get(id, businessID)
}

func (s *service) RecordPayment(id, businessID uuid.UUID, amount int64, note string) (*Supplier, error) {
	if amount <= 0 {
		return nil, ErrInvalidPayment
	}
	supplier, err := s.repo.RecordPayment(id, businessID, &Payment{Amount: amount, Note: note})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSupplierNotFound
	}
	return supplier, err
}
func (s *service) ListPayments(id, businessID uuid.UUID) ([]Payment, error) {
	return s.repo.ListPayments(id, businessID)
}
