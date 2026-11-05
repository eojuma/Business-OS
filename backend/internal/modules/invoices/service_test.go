package invoices

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeRepository struct {
	invoice    *Invoice
	findErr    error
	recordErr  error
	payment    *InvoicePayment
	lastFilter ListFilter
}

func (f *fakeRepository) FindByID(id, businessID uuid.UUID) (*Invoice, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.invoice, nil
}

func (f *fakeRepository) List(businessID uuid.UUID, filter ListFilter, now time.Time) ([]Invoice, int64, error) {
	f.lastFilter = filter
	if f.invoice == nil {
		return nil, 0, nil
	}
	return []Invoice{*f.invoice}, 1, nil
}

func (f *fakeRepository) RecordPayment(id, businessID uuid.UUID, payment *InvoicePayment) (*Invoice, error) {
	f.payment = payment
	if f.recordErr != nil {
		return nil, f.recordErr
	}
	invoice := *f.invoice
	invoice.AmountPaid += payment.Amount
	invoice.Status = StatusFor(invoice.TotalAmount, invoice.AmountPaid)
	return &invoice, nil
}

func TestNormalizeFilter(t *testing.T) {
	filter, err := NormalizeFilter(ListFilter{})
	if err != nil {
		t.Fatalf("normalize empty filter: %v", err)
	}
	if filter.Page != 1 || filter.Limit != 20 {
		t.Fatalf("defaults = page %d limit %d", filter.Page, filter.Limit)
	}

	filter, err = NormalizeFilter(ListFilter{Status: string(StatusOverdue)})
	if err != nil {
		t.Fatalf("normalize overdue: %v", err)
	}
	if !filter.Overdue || filter.Status != "" {
		t.Fatalf("overdue filter = %+v", filter)
	}

	if _, err := NormalizeFilter(ListFilter{Status: "void"}); !errors.Is(err, ErrInvalidStatusFilter) {
		t.Fatalf("invalid status error = %v", err)
	}

	from := time.Now()
	to := from.Add(-time.Hour)
	if _, err := NormalizeFilter(ListFilter{DateFrom: &from, DateTo: &to}); !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("invalid range error = %v", err)
	}
}

func TestRecordPaymentValidatesAmountBeforeRepository(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo)

	if _, err := svc.RecordPayment(uuid.New(), uuid.New(), RecordPaymentInput{Amount: 0}); !errors.Is(err, ErrInvalidPayment) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidPayment)
	}
	if repo.payment != nil {
		t.Fatal("repository must not be called for a non-positive payment")
	}
}

func TestRecordPaymentMapsMissingInvoice(t *testing.T) {
	repo := &fakeRepository{recordErr: gorm.ErrRecordNotFound}
	svc := NewService(repo)

	if _, err := svc.RecordPayment(uuid.New(), uuid.New(), RecordPaymentInput{Amount: 10}); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrInvoiceNotFound)
	}
}

func TestListComputesRemainingAndNormalizesFilter(t *testing.T) {
	due := time.Now().Add(-time.Hour)
	repo := &fakeRepository{invoice: &Invoice{TotalAmount: 1000, AmountPaid: 400, DueDate: due, Status: StatusPartial}}
	svc := NewService(repo)

	invoices, total, err := svc.List(uuid.New(), ListFilter{Status: string(StatusOverdue)})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(invoices) != 1 {
		t.Fatalf("total=%d len=%d", total, len(invoices))
	}
	if invoices[0].RemainingBalance != 600 || !invoices[0].IsOverdue {
		t.Fatalf("computed invoice = %+v", invoices[0])
	}
	if repo.lastFilter.Page != 1 || repo.lastFilter.Limit != 20 || !repo.lastFilter.Overdue {
		t.Fatalf("normalized filter = %+v", repo.lastFilter)
	}
}

func TestServiceGetMapsMissingInvoice(t *testing.T) {
	svc := NewService(&fakeRepository{findErr: gorm.ErrRecordNotFound})
	if _, err := svc.Get(uuid.New(), uuid.New()); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrInvoiceNotFound)
	}
}
