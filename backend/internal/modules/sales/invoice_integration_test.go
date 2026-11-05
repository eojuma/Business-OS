package sales

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type noopInventory struct{}

func (noopInventory) RecordMovementTx(*gorm.DB, uuid.UUID, uuid.UUID, string, int64, string) error {
	return nil
}

type noopCustomerCharger struct{}

func (noopCustomerCharger) ChargeCreditTx(*gorm.DB, uuid.UUID, uuid.UUID, int64) error {
	return nil
}

func setupSalesInvoiceTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
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

	schemaName := fmt.Sprintf("test_sales_invoices_%s", uuid.New().String()[:8])
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
	testBusiness := business.Business{ID: uuid.New(), Name: "Sales Invoice Business"}
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

func TestRepositoryCreateSaleCreatesInvoice(t *testing.T) {
	db, businessID := setupSalesInvoiceTestDB(t)
	product := &products.Product{BusinessID: businessID, Name: "Invoice Product", Unit: "unit", Price: 500}
	customer := &customers.Customer{BusinessID: businessID, Name: "Invoice Customer", CreditLimit: 1_000_000}
	for _, row := range []any{product, customer} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	sale := &Sale{BusinessID: businessID, CustomerID: &customer.ID, SaleType: SaleTypeCredit, TotalAmount: 500}
	items := []SaleLineItem{{ProductID: product.ID, Quantity: 1, UnitPrice: 500, Subtotal: 500}}
	if err := NewRepository(db).CreateSale(sale, items, noopInventory{}, noopCustomerCharger{}); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	if sale.Invoice == nil {
		t.Fatal("expected an invoice on the sale")
	}
	if sale.Invoice.InvoiceNumber != fmt.Sprintf("INV-%d-0001", time.Now().Year()) {
		t.Fatalf("invoice number = %q", sale.Invoice.InvoiceNumber)
	}
	if sale.Invoice.Status != invoices.StatusUnpaid || sale.Invoice.TotalAmount != 500 {
		t.Fatalf("invoice = %+v", sale.Invoice)
	}

	quotation := &Sale{BusinessID: businessID, SaleType: SaleTypeQuotation, TotalAmount: 100}
	quoteItems := []SaleLineItem{{ProductID: product.ID, Quantity: 1, UnitPrice: 100, Subtotal: 100}}
	if err := NewRepository(db).CreateSale(quotation, quoteItems, noopInventory{}, noopCustomerCharger{}); err != nil {
		t.Fatalf("create quotation: %v", err)
	}
	if quotation.Invoice != nil {
		t.Fatal("quotations must not create invoices")
	}
}
