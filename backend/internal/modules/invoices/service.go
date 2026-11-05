package invoices

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvoiceNotFound       = errors.New("invoice not found")
	ErrInvalidPayment        = errors.New("payment amount must be positive")
	ErrPaymentExceedsBalance = errors.New("payment exceeds the remaining invoice balance")
	ErrInvoiceAlreadyPaid    = errors.New("invoice is already paid")
	ErrInvalidStatusFilter   = errors.New("status must be unpaid, partial, paid or overdue")
	ErrInvalidDateRange      = errors.New("from date must not be after to date")
)

// RecordPaymentInput is a payment applied to one invoice.
type RecordPaymentInput struct {
	Amount int64
	Note   string
}

// Service exposes invoice reads and payment application.
type Service interface {
	List(businessID uuid.UUID, filter ListFilter) ([]Invoice, int64, error)
	Get(id, businessID uuid.UUID) (*Invoice, error)
	RecordPayment(id, businessID uuid.UUID, input RecordPaymentInput) (*Invoice, error)
}

type service struct {
	repo Repository
}

// NewService builds the invoice service.
func NewService(repo Repository) Service { return &service{repo: repo} }

// List returns normalized, computed invoices for the business.
func (s *service) List(businessID uuid.UUID, filter ListFilter) ([]Invoice, int64, error) {
	filter, err := NormalizeFilter(filter)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	invoices, total, err := s.repo.List(businessID, filter, now)
	if err != nil {
		return nil, 0, err
	}
	for i := range invoices {
		invoices[i].Compute(now)
	}
	return invoices, total, nil
}

// Get returns one computed invoice or ErrInvoiceNotFound.
func (s *service) Get(id, businessID uuid.UUID) (*Invoice, error) {
	invoice, err := s.repo.FindByID(id, businessID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvoiceNotFound
	}
	if err != nil {
		return nil, err
	}
	invoice.Compute(time.Now())
	return invoice, nil
}

// RecordPayment validates and applies a settlement to one invoice.
func (s *service) RecordPayment(id, businessID uuid.UUID, input RecordPaymentInput) (*Invoice, error) {
	if input.Amount <= 0 {
		return nil, ErrInvalidPayment
	}
	payment := &InvoicePayment{Amount: input.Amount, Note: strings.TrimSpace(input.Note)}
	invoice, err := s.repo.RecordPayment(id, businessID, payment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvoiceNotFound
	}
	if err != nil {
		return nil, err
	}
	invoice.Compute(time.Now())
	return invoice, nil
}
