package suppliers

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrSupplierNotFound      = errors.New("supplier not found")
	ErrInvalidPayment        = errors.New("payment amount must be positive")
	ErrPaymentExceedsBalance = errors.New("payment exceeds supplier outstanding balance")
	ErrInvalidPaymentTerms   = errors.New("invalid payment terms")
	ErrSupplierHasPurchases  = errors.New("supplier has purchases and cannot be archived")
	ErrAgingTotalOverflow    = errors.New("supplier aging total exceeds the supported amount")
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
	ListPaginated(businessID uuid.UUID, page, limit int, search string, includeArchived bool) ([]Supplier, int64, error)
	GetAging(businessID uuid.UUID, asOf time.Time) (*AgingReport, error)
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

func (s *service) ListPaginated(businessID uuid.UUID, page, limit int, search string, includeArchived bool) ([]Supplier, int64, error) {
	return s.repo.ListPaginated(businessID, page, limit, strings.TrimSpace(search), includeArchived)
}

func (s *service) GetAging(businessID uuid.UUID, asOf time.Time) (*AgingReport, error) {
	rows, err := s.repo.GetAging(businessID, asOf.UTC())
	if err != nil {
		return nil, err
	}
	report := &AgingReport{
		AsOf:    asOf.UTC(),
		Current: []AgingSupplier{}, Days1To30: []AgingSupplier{},
		Days31To60: []AgingSupplier{}, Days61To90: []AgingSupplier{}, Days90Plus: []AgingSupplier{},
		Totals: map[AgingBucket]int64{
			AgingCurrent: 0, AgingDays1To30: 0, AgingDays31To60: 0, AgingDays61To90: 0, AgingDays90Plus: 0,
		},
	}
	for _, row := range rows {
		var bucket *[]AgingSupplier
		switch row.Bucket {
		case AgingCurrent:
			bucket = &report.Current
		case AgingDays1To30:
			bucket = &report.Days1To30
		case AgingDays31To60:
			bucket = &report.Days31To60
		case AgingDays61To90:
			bucket = &report.Days61To90
		case AgingDays90Plus:
			bucket = &report.Days90Plus
		default:
			return nil, fmt.Errorf("unknown supplier aging bucket %q", row.Bucket)
		}
		if row.OutstandingBalance > math.MaxInt64-report.Totals[row.Bucket] {
			return nil, ErrAgingTotalOverflow
		}
		*bucket = append(*bucket, row)
		report.Totals[row.Bucket] += row.OutstandingBalance
	}
	return report, nil
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
