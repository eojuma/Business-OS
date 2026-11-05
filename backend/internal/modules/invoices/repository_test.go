package invoices

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupInvoicesTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
	t.Helper()

	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	parsedURL, err := url.Parse(testDBURL)
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User == nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}
	if dbName := strings.TrimPrefix(parsedURL.Path, "/"); dbName == "" {
		t.Fatal("invalid TEST_DATABASE_URL: database name is required")
	}

	db := database.NewPostgresFromURL(testDBURL)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("database not reachable: %v", err)
	}

	schemaName := fmt.Sprintf("test_invoices_%s", uuid.New().String()[:8])
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error; err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	db = db.Session(&gorm.Session{NewDB: true})
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName)).Error; err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	testBusiness := business.Business{ID: uuid.New(), Name: "Invoice Test Business"}
	if err := db.Create(&testBusiness).Error; err != nil {
		t.Fatalf("create business: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error; err != nil {
			t.Logf("cleanup warning: %v", err)
		}
	})
	return db, testBusiness.ID
}

func seedCustomer(t *testing.T, db *gorm.DB, businessID uuid.UUID, balance int64) uuid.UUID {
	t.Helper()
	customer := &customers.Customer{BusinessID: businessID, Name: "Invoice Customer", Balance: balance, CreditLimit: 1_000_000}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	return customer.ID
}

func seedSale(t *testing.T, db *gorm.DB, businessID, customerID uuid.UUID, saleType string, total, discount int64, issue, due time.Time) *Invoice {
	t.Helper()
	saleID := uuid.New()
	if err := db.Exec(
		`INSERT INTO sales (id, business_id, customer_id, sale_type, total_amount, discount) VALUES (?, ?, ?, ?, ?, ?)`,
		saleID, businessID, customerID, saleType, total, discount,
	).Error; err != nil {
		t.Fatalf("create sale: %v", err)
	}
	var invoice *Invoice
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		invoice, err = CreateForSaleTx(tx, CreateForSaleInput{
			BusinessID: businessID,
			SaleID:     saleID,
			CustomerID: &customerID,
			SaleType:   saleType,
			IssueDate:  issue,
			DueDate:    due,
			Subtotal:   total + discount,
			Discount:   discount,
			Total:      total,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	return invoice
}

func TestCreateForSaleTxUsesSequentialNumbersAndCashStatus(t *testing.T) {
	db, businessID := setupInvoicesTestDB(t)
	customerID := seedCustomer(t, db, businessID, 0)
	year := time.Now().Year()

	cash := seedSale(t, db, businessID, customerID, "cash", 1000, 100, time.Now(), time.Now())
	credit := seedSale(t, db, businessID, customerID, "credit", 2000, 0, time.Now(), time.Now().AddDate(0, 0, 30))

	if cash.InvoiceNumber != formatInvoiceNumber(year, 1) || credit.InvoiceNumber != formatInvoiceNumber(year, 2) {
		t.Fatalf("numbers = %q, %q", cash.InvoiceNumber, credit.InvoiceNumber)
	}
	if cash.Subtotal != 1100 || cash.Discount != 100 || cash.TotalAmount != 1000 {
		t.Fatalf("cash totals = subtotal %d discount %d total %d", cash.Subtotal, cash.Discount, cash.TotalAmount)
	}
	if cash.Status != StatusPaid || cash.AmountPaid != 1000 {
		t.Fatalf("cash invoice status=%q paid=%d", cash.Status, cash.AmountPaid)
	}
	if len(cash.Payments) != 1 || cash.Payments[0].Amount != 1000 {
		t.Fatalf("cash payments = %+v", cash.Payments)
	}
	if credit.Status != StatusUnpaid || credit.AmountPaid != 0 {
		t.Fatalf("credit invoice status=%q paid=%d", credit.Status, credit.AmountPaid)
	}

	otherBusiness := business.Business{ID: uuid.New(), Name: "Other Invoice Business"}
	if err := db.Create(&otherBusiness).Error; err != nil {
		t.Fatalf("create other business: %v", err)
	}
	otherCustomerID := seedCustomer(t, db, otherBusiness.ID, 0)
	other := seedSale(t, db, otherBusiness.ID, otherCustomerID, "cash", 500, 0, time.Now(), time.Now())
	if other.InvoiceNumber != formatInvoiceNumber(year, 1) {
		t.Fatalf("other business number = %q", other.InvoiceNumber)
	}
}

func TestNextInvoiceNumberResetsEachYear(t *testing.T) {
	db, businessID := setupInvoicesTestDB(t)
	first, err := nextInvoiceNumberTx(db, businessID, 2026)
	if err != nil {
		t.Fatalf("first 2026: %v", err)
	}
	second, err := nextInvoiceNumberTx(db, businessID, 2026)
	if err != nil {
		t.Fatalf("second 2026: %v", err)
	}
	nextYear, err := nextInvoiceNumberTx(db, businessID, 2027)
	if err != nil {
		t.Fatalf("first 2027: %v", err)
	}
	if first != "INV-2026-0001" || second != "INV-2026-0002" || nextYear != "INV-2027-0001" {
		t.Fatalf("numbers = %q, %q, %q", first, second, nextYear)
	}
}

func TestRecordPaymentTransitionsStatusAndBalance(t *testing.T) {
	db, businessID := setupInvoicesTestDB(t)
	customerID := seedCustomer(t, db, businessID, 1000)
	invoice := seedSale(t, db, businessID, customerID, "credit", 1000, 0, time.Now(), time.Now().AddDate(0, 0, 30))
	repo := NewRepository(db)

	partial, err := repo.RecordPayment(invoice.ID, businessID, &InvoicePayment{Amount: 400, Note: "first"})
	if err != nil {
		t.Fatalf("partial payment: %v", err)
	}
	if partial.Status != StatusPartial || partial.AmountPaid != 400 || partial.RemainingBalance != 600 {
		t.Fatalf("partial status=%q paid=%d remaining=%d", partial.Status, partial.AmountPaid, partial.RemainingBalance)
	}
	var customer customers.Customer
	if err := db.First(&customer, "id = ?", customerID).Error; err != nil {
		t.Fatalf("find customer: %v", err)
	}
	if customer.Balance != 600 {
		t.Fatalf("customer balance = %d, want 600", customer.Balance)
	}

	paid, err := repo.RecordPayment(invoice.ID, businessID, &InvoicePayment{Amount: 600, Note: "final"})
	if err != nil {
		t.Fatalf("final payment: %v", err)
	}
	if paid.Status != StatusPaid || paid.AmountPaid != 1000 || paid.RemainingBalance != 0 {
		t.Fatalf("paid status=%q paid=%d remaining=%d", paid.Status, paid.AmountPaid, paid.RemainingBalance)
	}
	if len(paid.Payments) != 2 {
		t.Fatalf("payments = %d, want 2", len(paid.Payments))
	}

	if _, err := repo.RecordPayment(invoice.ID, businessID, &InvoicePayment{Amount: 1}); !errors.Is(err, ErrInvoiceAlreadyPaid) {
		t.Fatalf("payment after paid error = %v, want %v", err, ErrInvoiceAlreadyPaid)
	}
}

func TestRecordPaymentRejectsOverpayment(t *testing.T) {
	db, businessID := setupInvoicesTestDB(t)
	customerID := seedCustomer(t, db, businessID, 1000)
	invoice := seedSale(t, db, businessID, customerID, "credit", 1000, 0, time.Now(), time.Now())
	repo := NewRepository(db)

	if _, err := repo.RecordPayment(invoice.ID, businessID, &InvoicePayment{Amount: 1001}); !errors.Is(err, ErrPaymentExceedsBalance) {
		t.Fatalf("overpayment error = %v, want %v", err, ErrPaymentExceedsBalance)
	}
}

func TestListFiltersAndOverdue(t *testing.T) {
	db, businessID := setupInvoicesTestDB(t)
	customerA := seedCustomer(t, db, businessID, 0)
	customerB := seedCustomer(t, db, businessID, 0)
	now := time.Now()

	overdue := seedSale(t, db, businessID, customerA, "credit", 500, 0, now.AddDate(0, 0, -10), now.AddDate(0, 0, -1))
	_ = seedSale(t, db, businessID, customerB, "credit", 700, 0, now, now.AddDate(0, 0, 30))

	repo := NewRepository(db)
	overdueInvoices, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, Overdue: true}, now)
	if err != nil {
		t.Fatalf("list overdue: %v", err)
	}
	if total != 1 || len(overdueInvoices) != 1 || overdueInvoices[0].ID != overdue.ID {
		t.Fatalf("overdue total=%d items=%d", total, len(overdueInvoices))
	}

	byCustomer, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, CustomerID: &customerB}, now)
	if err != nil {
		t.Fatalf("list by customer: %v", err)
	}
	if total != 1 || len(byCustomer) != 1 || byCustomer[0].CustomerID == nil || *byCustomer[0].CustomerID != customerB {
		t.Fatalf("customer filter total=%d items=%d", total, len(byCustomer))
	}

	from := now.AddDate(0, 0, -2)
	recent, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, DateFrom: &from}, now)
	if err != nil {
		t.Fatalf("list by date: %v", err)
	}
	if total != 1 || len(recent) != 1 {
		t.Fatalf("date filter total=%d items=%d", total, len(recent))
	}
}

func TestStatusFor(t *testing.T) {
	tests := []struct {
		total, paid int64
		want        Status
	}{
		{1000, 0, StatusUnpaid},
		{1000, 1, StatusPartial},
		{1000, 999, StatusPartial},
		{1000, 1000, StatusPaid},
		{0, 0, StatusPaid},
	}
	for _, tt := range tests {
		if got := StatusFor(tt.total, tt.paid); got != tt.want {
			t.Fatalf("StatusFor(%d, %d) = %q, want %q", tt.total, tt.paid, got, tt.want)
		}
	}
}
