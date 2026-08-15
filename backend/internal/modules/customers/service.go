package customers

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrCustomerNotFound      = errors.New("customer not found")
	ErrCreditLimitExceeded   = errors.New("this would exceed the customer's credit limit")
	ErrInvalidPayment        = errors.New("payment amount must be positive")
	ErrPaymentExceedsBalance = errors.New("payment exceeds customer balance")
)

type CreateInput struct {
	BusinessID  uuid.UUID
	Name        string
	Phone       string
	Email       string
	CreditLimit int64
}

type UpdateInput struct {
	Name        *string
	Phone       *string
	Email       *string
	CreditLimit *int64
}

type Service interface {
	Create(input CreateInput) (*Customer, error)
	Get(id, businessID uuid.UUID) (*Customer, error)
	List(businessID uuid.UUID) ([]Customer, error)
	Update(id, businessID uuid.UUID, input UpdateInput) (*Customer, error)
	ListAboveBalance(businessID uuid.UUID, threshold int64) ([]Customer, error)
	RecordPayment(id, businessID uuid.UUID, amount int64, note string) (*Customer, error)
	ListPayments(id, businessID uuid.UUID) ([]Payment, error)
	// ChargeCreditTx increases a customer's balance for a credit sale,
	// refusing if it would exceed their credit limit. Called from sales
	// inside the same transaction as the sale itself.
	ChargeCreditTx(tx *gorm.DB, businessID, customerID uuid.UUID, amount int64) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(input CreateInput) (*Customer, error) {
	c := &Customer{
		BusinessID:  input.BusinessID,
		Name:        input.Name,
		Phone:       input.Phone,
		Email:       input.Email,
		CreditLimit: input.CreditLimit,
	}
	if err := s.repo.Create(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *service) Get(id, businessID uuid.UUID) (*Customer, error) {
	c, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrCustomerNotFound
	}
	return c, nil
}

func (s *service) List(businessID uuid.UUID) ([]Customer, error) {
	return s.repo.List(businessID)
}

func (s *service) Update(id, businessID uuid.UUID, input UpdateInput) (*Customer, error) {
	c, err := s.repo.FindByID(id, businessID)
	if err != nil {
		return nil, ErrCustomerNotFound
	}

	if input.Name != nil {
		c.Name = *input.Name
	}
	if input.Phone != nil {
		c.Phone = *input.Phone
	}
	if input.Email != nil {
		c.Email = *input.Email
	}
	if input.CreditLimit != nil {
		c.CreditLimit = *input.CreditLimit
	}

	if err := s.repo.Update(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *service) ListAboveBalance(businessID uuid.UUID, threshold int64) ([]Customer, error) {
	return s.repo.ListAboveBalance(businessID, threshold)
}

func (s *service) RecordPayment(id, businessID uuid.UUID, amount int64, note string) (*Customer, error) {
	if amount <= 0 {
		return nil, ErrInvalidPayment
	}
	c, err := s.repo.RecordPayment(id, businessID, &Payment{Amount: amount, Note: note})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCustomerNotFound
	}
	return c, err
}

func (s *service) ListPayments(id, businessID uuid.UUID) ([]Payment, error) {
	return s.repo.ListPayments(id, businessID)
}

func (s *service) ChargeCreditTx(tx *gorm.DB, businessID, customerID uuid.UUID, amount int64) error {
	var c Customer
	if err := tx.Where("id = ? AND business_id = ?", customerID, businessID).First(&c).Error; err != nil {
		return ErrCustomerNotFound
	}

	if !c.HasAvailableCredit(amount) {
		return ErrCreditLimitExceeded
	}

	c.Balance += amount
	return tx.Save(&c).Error
}
