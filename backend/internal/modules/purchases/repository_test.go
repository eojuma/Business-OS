package purchases

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

func setupTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
	t.Helper()

	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	parsedURL, err := url.Parse(testDBURL)
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User == nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}

	cfg := config.Load()
	cfg.DBHost = parsedURL.Hostname()
	if port := parsedURL.Port(); port != "" {
		cfg.DBPort = port
	}
	cfg.DBUser = parsedURL.User.Username()
	cfg.DBPassword = ""
	if password, ok := parsedURL.User.Password(); ok {
		cfg.DBPassword = password
	}
	if dbName := strings.TrimPrefix(parsedURL.Path, "/"); dbName != "" {
		cfg.DBName = dbName
	}
	if sslMode := parsedURL.Query().Get("sslmode"); sslMode != "" {
		cfg.DBSSLMode = sslMode
	}

	db := database.NewPostgres(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Skipf("Database not reachable, skipping integration test: %v", err)
	}

	// Use a unique schema per test so packages can run in parallel safely.
	schemaName := fmt.Sprintf("test_purchases_%s", uuid.New().String()[:8])
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error; err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	db = db.Session(&gorm.Session{NewDB: true})
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName)).Error; err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatalf("failed to run migrations in test schema: %v", err)
	}

	testBusiness := business.Business{ID: uuid.New(), Name: "Test Business"}
	if err := db.Create(&testBusiness).Error; err != nil {
		t.Fatalf("failed to create test business: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error; err != nil {
			t.Logf("cleanup warning: failed to drop schema %s: %v", schemaName, err)
		}
	})

	return db, testBusiness.ID
}

type purchaseFixture struct {
	purchase   *Purchase
	productA   uuid.UUID
	productB   uuid.UUID
	supplierID uuid.UUID
}

func seedPurchase(t *testing.T, db *gorm.DB, businessID uuid.UUID, quantityA, quantityB int64) purchaseFixture {
	t.Helper()

	supplier := &suppliers.Supplier{BusinessID: businessID, Name: "Test Supplier", PaymentTerms: suppliers.PaymentTermsNet30}
	if err := db.Create(supplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}

	productA := &products.Product{BusinessID: businessID, Name: "Product A", Unit: "unit", Price: 300}
	productB := &products.Product{BusinessID: businessID, Name: "Product B", Unit: "unit", Price: 400}
	if err := db.Create(productA).Error; err != nil {
		t.Fatalf("create product A: %v", err)
	}
	if err := db.Create(productB).Error; err != nil {
		t.Fatalf("create product B: %v", err)
	}

	purchase := &Purchase{
		BusinessID:  businessID,
		SupplierID:  supplier.ID,
		Status:      "draft",
		TotalAmount: quantityA*100 + quantityB*200,
	}
	items := []PurchaseLineItem{
		{ProductID: productA.ID, Quantity: quantityA, UnitCost: 100, Subtotal: quantityA * 100},
		{ProductID: productB.ID, Quantity: quantityB, UnitCost: 200, Subtotal: quantityB * 200},
	}
	if err := NewRepository(db).Create(purchase, items); err != nil {
		t.Fatalf("create purchase: %v", err)
	}

	return purchaseFixture{purchase: purchase, productA: productA.ID, productB: productB.ID, supplierID: supplier.ID}
}

func receivePurchase(t *testing.T, db *gorm.DB, businessID, purchaseID uuid.UUID, items []ReceiveItemInput) (*Purchase, error) {
	t.Helper()
	return NewRepository(db).Receive(
		purchaseID,
		businessID,
		items,
		inventory.NewRepository(db),
		suppliers.NewRepository(db),
		products.NewRepository(db),
	)
}

func receivedQty(t *testing.T, db *gorm.DB, purchaseID, productID uuid.UUID) int64 {
	t.Helper()
	var item PurchaseLineItem
	if err := db.Where("purchase_id = ? AND product_id = ?", purchaseID, productID).First(&item).Error; err != nil {
		t.Fatalf("find line item: %v", err)
	}
	return item.ReceivedQty
}

func stockQty(t *testing.T, db *gorm.DB, businessID, productID uuid.UUID) int64 {
	t.Helper()
	level, err := inventory.NewRepository(db).GetStockLevel(productID, businessID)
	if err != nil {
		t.Fatalf("get stock level: %v", err)
	}
	return level.Quantity
}

func supplierBalance(t *testing.T, db *gorm.DB, supplierID, businessID uuid.UUID) int64 {
	t.Helper()
	supplier, err := suppliers.NewRepository(db).FindByID(supplierID, businessID)
	if err != nil {
		t.Fatalf("find supplier: %v", err)
	}
	return supplier.OutstandingBalance
}

func TestRepositoryReceivePartialShipments(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 5, 4)

	// First shipment: 2 of A and 1 of B.
	first, err := receivePurchase(t, db, businessID, fixture.purchase.ID, []ReceiveItemInput{
		{ProductID: fixture.productA, ReceivedQty: 2},
		{ProductID: fixture.productB, ReceivedQty: 1},
	})
	if err != nil {
		t.Fatalf("partial receive: %v", err)
	}
	if first.Status != "draft" {
		t.Fatalf("status = %q, want draft until fully received", first.Status)
	}
	if first.ReceivedAt != nil {
		t.Fatalf("received_at = %v, want nil for a partial receipt", first.ReceivedAt)
	}
	if first.DueDate == nil {
		t.Fatal("expected a due date on the purchase after the first receipt")
	}
	if got := receivedQty(t, db, fixture.purchase.ID, fixture.productA); got != 2 {
		t.Fatalf("A received_qty = %d, want 2", got)
	}
	if got := receivedQty(t, db, fixture.purchase.ID, fixture.productB); got != 1 {
		t.Fatalf("B received_qty = %d, want 1", got)
	}
	if got := stockQty(t, db, businessID, fixture.productA); got != 2 {
		t.Fatalf("A stock = %d, want 2", got)
	}
	if got := stockQty(t, db, businessID, fixture.productB); got != 1 {
		t.Fatalf("B stock = %d, want 1", got)
	}
	if got := supplierBalance(t, db, fixture.supplierID, businessID); got != 2*100+1*200 {
		t.Fatalf("supplier balance = %d, want 400 (only delivered value)", got)
	}

	// Second shipment omits items, so all remaining quantities are received.
	second, err := receivePurchase(t, db, businessID, fixture.purchase.ID, nil)
	if err != nil {
		t.Fatalf("final receive: %v", err)
	}
	if second.Status != "received" {
		t.Fatalf("status = %q, want received", second.Status)
	}
	if second.ReceivedAt == nil {
		t.Fatal("expected received_at to be set once fully received")
	}
	if got := receivedQty(t, db, fixture.purchase.ID, fixture.productA); got != 5 {
		t.Fatalf("A received_qty = %d, want 5", got)
	}
	if got := receivedQty(t, db, fixture.purchase.ID, fixture.productB); got != 4 {
		t.Fatalf("B received_qty = %d, want 4", got)
	}
	if got := stockQty(t, db, businessID, fixture.productA); got != 5 {
		t.Fatalf("A stock = %d, want 5", got)
	}
	if got := stockQty(t, db, businessID, fixture.productB); got != 4 {
		t.Fatalf("B stock = %d, want 4", got)
	}
	if got := supplierBalance(t, db, fixture.supplierID, businessID); got != 5*100+4*200 {
		t.Fatalf("supplier balance = %d, want 1300", got)
	}

	// A fully received purchase cannot be received again.
	if _, err := receivePurchase(t, db, businessID, fixture.purchase.ID, nil); !errors.Is(err, ErrAlreadyReceived) {
		t.Fatalf("error = %v, want %v", err, ErrAlreadyReceived)
	}
}

func TestRepositoryReceiveRejectsOverReceipt(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 5, 4)

	_, err := receivePurchase(t, db, businessID, fixture.purchase.ID, []ReceiveItemInput{
		{ProductID: fixture.productA, ReceivedQty: 6},
	})
	if !errors.Is(err, ErrInvalidReceiveQuantity) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidReceiveQuantity)
	}

	// The transaction rolled back: nothing was applied.
	stored, err := NewRepository(db).FindByID(fixture.purchase.ID, businessID)
	if err != nil {
		t.Fatalf("find purchase: %v", err)
	}
	if stored.Status != "draft" {
		t.Fatalf("status = %q, want draft", stored.Status)
	}
	for _, item := range stored.Items {
		if item.ReceivedQty != 0 {
			t.Fatalf("line item %s received_qty = %d, want 0", item.ProductID, item.ReceivedQty)
		}
	}
	var movements int64
	if err := db.Model(&inventory.StockMovement{}).Where("business_id = ?", businessID).Count(&movements).Error; err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if movements != 0 {
		t.Fatalf("stock movements = %d, want 0", movements)
	}
	if got := supplierBalance(t, db, fixture.supplierID, businessID); got != 0 {
		t.Fatalf("supplier balance = %d, want 0", got)
	}
}

func TestRepositoryReceiveProratesAmountPaid(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 5, 4)

	// Total is 1300; paying 500 up front means only 800 should ever be charged.
	if err := db.Model(&Purchase{}).Where("id = ?", fixture.purchase.ID).Update("amount_paid", 500).Error; err != nil {
		t.Fatalf("set amount paid: %v", err)
	}

	// First shipment value is 400, fully covered by the prepayment.
	if _, err := receivePurchase(t, db, businessID, fixture.purchase.ID, []ReceiveItemInput{
		{ProductID: fixture.productA, ReceivedQty: 2},
		{ProductID: fixture.productB, ReceivedQty: 1},
	}); err != nil {
		t.Fatalf("partial receive: %v", err)
	}
	if got := supplierBalance(t, db, fixture.supplierID, businessID); got != 0 {
		t.Fatalf("supplier balance after prepaid shipment = %d, want 0", got)
	}

	// Final shipment pushes the delivered value past the prepayment.
	if _, err := receivePurchase(t, db, businessID, fixture.purchase.ID, nil); err != nil {
		t.Fatalf("final receive: %v", err)
	}
	if got := supplierBalance(t, db, fixture.supplierID, businessID); got != 800 {
		t.Fatalf("supplier balance = %d, want 800", got)
	}
}

func TestRepositoryReceiveAllRemainingWhenItemsOmitted(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 3, 2)

	purchase, err := receivePurchase(t, db, businessID, fixture.purchase.ID, nil)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if purchase.Status != "received" {
		t.Fatalf("status = %q, want received", purchase.Status)
	}
	if got := stockQty(t, db, businessID, fixture.productA); got != 3 {
		t.Fatalf("A stock = %d, want 3", got)
	}
	if got := stockQty(t, db, businessID, fixture.productB); got != 2 {
		t.Fatalf("B stock = %d, want 2", got)
	}
	if got := supplierBalance(t, db, fixture.supplierID, businessID); got != 3*100+2*200 {
		t.Fatalf("supplier balance = %d, want 700", got)
	}
}

func TestRepositoryCreatePersistsExpectedDateAndLineNotes(t *testing.T) {
	db, businessID := setupTestDB(t)

	supplier := &suppliers.Supplier{BusinessID: businessID, Name: "Notes Supplier", PaymentTerms: suppliers.PaymentTermsNet30}
	if err := db.Create(supplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	product := &products.Product{BusinessID: businessID, Name: "Product", Unit: "unit", Price: 200}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	expectedAt := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	purchase := &Purchase{
		BusinessID:  businessID,
		SupplierID:  supplier.ID,
		Status:      "draft",
		TotalAmount: 200,
		ExpectedAt:  &expectedAt,
	}
	items := []PurchaseLineItem{
		{ProductID: product.ID, Quantity: 1, UnitCost: 200, Subtotal: 200, Note: "fragile"},
	}
	if err := NewRepository(db).Create(purchase, items); err != nil {
		t.Fatalf("create purchase: %v", err)
	}

	found, err := NewRepository(db).FindByID(purchase.ID, businessID)
	if err != nil {
		t.Fatalf("find purchase: %v", err)
	}
	if found.ExpectedAt == nil || !found.ExpectedAt.UTC().Equal(expectedAt) {
		t.Fatalf("expected_at = %v, want %v", found.ExpectedAt, expectedAt)
	}
	if len(found.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(found.Items))
	}
	if found.Items[0].Note != "fragile" {
		t.Fatalf("item note = %q, want %q", found.Items[0].Note, "fragile")
	}
}
