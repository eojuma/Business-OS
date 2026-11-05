package invoices

import (
	"fmt"
	"time"

	"github.com/businessos/backend/internal/modules/customers"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockForUpdate is the row-lock mode used while applying a payment.
const lockForUpdate = "UPDATE"

// CreateForSaleInput carries the sale totals needed to create its invoice.
type CreateForSaleInput struct {
	BusinessID uuid.UUID
	SaleID     uuid.UUID
	CustomerID *uuid.UUID
	SaleType   string
	IssueDate  time.Time
	DueDate    time.Time
	Subtotal   int64
	Discount   int64
	Total      int64
}

// ListFilter narrows the invoice list.
type ListFilter struct {
	Page       int
	Limit      int
	Status     string
	CustomerID *uuid.UUID
	DateFrom   *time.Time
	DateTo     *time.Time
	Overdue    bool
}

// Repository persists invoices and their payments.
type Repository interface {
	FindByID(id, businessID uuid.UUID) (*Invoice, error)
	List(businessID uuid.UUID, filter ListFilter, now time.Time) ([]Invoice, int64, error)
	RecordPayment(id, businessID uuid.UUID, payment *InvoicePayment) (*Invoice, error)
}

type repository struct{ db *gorm.DB }

// NewRepository builds an invoice repository over the given database.
func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

// CreateForSaleTx creates the invoice inside the sale transaction so a sale can
// never commit without its receivable document. Cash sales are recorded as
// immediately paid.
func CreateForSaleTx(tx *gorm.DB, input CreateForSaleInput) (*Invoice, error) {
	if input.SaleType == "quotation" {
		return nil, nil
	}
	if input.IssueDate.IsZero() {
		input.IssueDate = time.Now()
	}
	if input.DueDate.IsZero() {
		input.DueDate = input.IssueDate
	}

	number, err := nextInvoiceNumberTx(tx, input.BusinessID, input.IssueDate.Year())
	if err != nil {
		return nil, err
	}

	status := StatusFor(input.Total, 0)
	amountPaid := int64(0)
	if input.SaleType == "cash" || input.Total == 0 {
		status = StatusPaid
		amountPaid = input.Total
	}

	invoice := &Invoice{
		BusinessID:    input.BusinessID,
		SaleID:        input.SaleID,
		InvoiceNumber: number,
		CustomerID:    input.CustomerID,
		Status:        status,
		IssueDate:     input.IssueDate,
		DueDate:       input.DueDate,
		Subtotal:      input.Subtotal,
		Discount:      input.Discount,
		TotalAmount:   input.Total,
		AmountPaid:    amountPaid,
	}
	if err := tx.Create(invoice).Error; err != nil {
		return nil, err
	}

	invoice.Compute(input.IssueDate)
	if amountPaid > 0 {
		payment := &InvoicePayment{
			BusinessID: input.BusinessID,
			InvoiceID:  invoice.ID,
			Amount:     amountPaid,
			Note:       "Cash sale",
			PaidAt:     input.IssueDate,
		}
		if err := tx.Create(payment).Error; err != nil {
			return nil, err
		}
		invoice.Payments = []InvoicePayment{*payment}
	}
	return invoice, nil
}

// formatInvoiceNumber renders INV-{year}-{sequence padded to four digits}.
func formatInvoiceNumber(year int, sequence int64) string {
	return fmt.Sprintf("INV-%d-%04d", year, sequence)
}

// nextInvoiceNumberTx reserves the next per-business, per-year invoice number.
func nextInvoiceNumberTx(tx *gorm.DB, businessID uuid.UUID, year int) (string, error) {
	var sequence int64
	row := tx.Raw(`
		INSERT INTO invoice_number_counters (business_id, year, last_sequence)
		VALUES (?, ?, 1)
		ON CONFLICT (business_id, year)
		DO UPDATE SET last_sequence = invoice_number_counters.last_sequence + 1
		RETURNING last_sequence`, businessID, year).Row()
	if err := row.Scan(&sequence); err != nil {
		return "", err
	}
	return formatInvoiceNumber(year, sequence), nil
}

// FindByID loads one invoice with its customer and ordered payments.
func (r *repository) FindByID(id, businessID uuid.UUID) (*Invoice, error) {
	var invoice Invoice
	err := r.db.
		Preload("Customer").
		Preload("Payments", func(db *gorm.DB) *gorm.DB {
			return db.Order("paid_at asc, created_at asc")
		}).
		Where("id = ? AND business_id = ?", id, businessID).
		First(&invoice).Error
	if err != nil {
		return nil, err
	}
	invoice.Compute(time.Now())
	return &invoice, nil
}

// List applies invoice filters, pagination, and derived fields.
func (r *repository) List(businessID uuid.UUID, filter ListFilter, now time.Time) ([]Invoice, int64, error) {
	query := r.db.Model(&Invoice{}).Preload("Customer").Where("business_id = ?", businessID)
	if filter.Overdue {
		query = query.Where("due_date < ? AND status <> ?", now, StatusPaid)
	} else if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.CustomerID != nil {
		query = query.Where("customer_id = ?", *filter.CustomerID)
	}
	if filter.DateFrom != nil {
		query = query.Where("issue_date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query = query.Where("issue_date <= ?", *filter.DateTo)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var invoices []Invoice
	err := query.
		Order("issue_date desc, created_at desc").
		Offset((filter.Page - 1) * filter.Limit).
		Limit(filter.Limit).
		Find(&invoices).Error
	if err != nil {
		return nil, 0, err
	}
	for i := range invoices {
		invoices[i].Compute(now)
	}
	return invoices, total, nil
}

// RecordPayment applies a payment atomically, then recomputes the invoice
// status and reduces the customer's running balance.
func (r *repository) RecordPayment(id, businessID uuid.UUID, payment *InvoicePayment) (*Invoice, error) {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var invoice Invoice
		err := tx.Clauses(clause.Locking{Strength: lockForUpdate}).
			Where("id = ? AND business_id = ?", id, businessID).
			First(&invoice).Error
		if err != nil {
			return err
		}
		if payment.Amount <= 0 {
			return ErrInvalidPayment
		}
		remaining := invoice.TotalAmount - invoice.AmountPaid
		if remaining <= 0 {
			return ErrInvoiceAlreadyPaid
		}
		if payment.Amount > remaining {
			return ErrPaymentExceedsBalance
		}

		now := time.Now()
		if payment.PaidAt.IsZero() {
			payment.PaidAt = now
		}
		payment.BusinessID = businessID
		payment.InvoiceID = invoice.ID
		if err := tx.Create(payment).Error; err != nil {
			return err
		}

		invoice.AmountPaid += payment.Amount
		invoice.Status = StatusFor(invoice.TotalAmount, invoice.AmountPaid)
		if err := tx.Model(&Invoice{}).
			Where("id = ?", invoice.ID).
			Updates(map[string]any{
				"amount_paid": invoice.AmountPaid,
				"status":      invoice.Status,
				"updated_at":  now,
			}).Error; err != nil {
			return err
		}

		if invoice.CustomerID != nil {
			if err := tx.Model(&customers.Customer{}).
				Where("id = ? AND business_id = ?", *invoice.CustomerID, businessID).
				UpdateColumn("balance", gorm.Expr("CASE WHEN balance >= ? THEN balance - ? ELSE 0 END", payment.Amount, payment.Amount)).
				Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.FindByID(id, businessID)
}

// NormalizeFilter validates and normalizes list filters.
func NormalizeFilter(filter ListFilter) (ListFilter, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.DateFrom != nil && filter.DateTo != nil && filter.DateFrom.After(*filter.DateTo) {
		return filter, ErrInvalidDateRange
	}
	status := filter.Status
	switch status {
	case "", string(StatusUnpaid), string(StatusPartial), string(StatusPaid):
		filter.Status = status
	case string(StatusOverdue):
		filter.Status = ""
		filter.Overdue = true
	default:
		return filter, ErrInvalidStatusFilter
	}
	return filter, nil
}
