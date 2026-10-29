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

	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

const testStatusDraft = "draft"

func setupTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
	t.Helper()

	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	parsedURL, err := url.Parse(testDBURL)
	if err != nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}
	if parsedURL.Hostname() == "" {
		t.Fatalf("invalid TEST_DATABASE_URL: host is required")
	}
	if parsedURL.User == nil {
		t.Fatalf("invalid TEST_DATABASE_URL: user is required")
	}
	if dbName := strings.TrimPrefix(parsedURL.Path, "/"); dbName == "" {
		t.Fatalf("invalid TEST_DATABASE_URL: database name is required")
	}

	db := database.NewPostgresFromURL(testDBURL)

	// Pin the pool to a single connection so the session-level
	// SET search_path below applies to every query, including the
	// migration connection pinned by migrations.Up.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("Database not reachable: %v", err)
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
		Status:      testStatusDraft,
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
	if first.Status != testStatusDraft {
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
	if stored.Status != testStatusDraft {
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

func TestRepositoryCreateAssignsSequentialPONumbers(t *testing.T) {
	db, businessID := setupTestDB(t)
	year := time.Now().Year()

	first := seedPurchase(t, db, businessID, 1, 1)
	second := seedPurchase(t, db, businessID, 1, 1)

	wantFirst := formatPONumber(year, 1)
	wantSecond := formatPONumber(year, 2)
	if first.purchase.PONumber != wantFirst {
		t.Fatalf("first po_number = %q, want %q", first.purchase.PONumber, wantFirst)
	}
	if second.purchase.PONumber != wantSecond {
		t.Fatalf("second po_number = %q, want %q", second.purchase.PONumber, wantSecond)
	}

	found, err := NewRepository(db).FindByID(first.purchase.ID, businessID)
	if err != nil {
		t.Fatalf("find purchase: %v", err)
	}
	if found.PONumber != wantFirst {
		t.Fatalf("persisted po_number = %q, want %q", found.PONumber, wantFirst)
	}

	// Sequences are per business: another business starts again at 0001.
	otherBusiness := business.Business{ID: uuid.New(), Name: "Other Business"}
	if err := db.Create(&otherBusiness).Error; err != nil {
		t.Fatalf("create other business: %v", err)
	}
	other := seedPurchase(t, db, otherBusiness.ID, 1, 1)
	if other.purchase.PONumber != wantFirst {
		t.Fatalf("other business po_number = %q, want %q", other.purchase.PONumber, wantFirst)
	}
}

func TestNextPONumberResetsEachYear(t *testing.T) {
	db, businessID := setupTestDB(t)

	first2026, err := nextPONumberTx(db, businessID, 2026)
	if err != nil {
		t.Fatalf("next 2026 #1: %v", err)
	}
	second2026, err := nextPONumberTx(db, businessID, 2026)
	if err != nil {
		t.Fatalf("next 2026 #2: %v", err)
	}
	first2027, err := nextPONumberTx(db, businessID, 2027)
	if err != nil {
		t.Fatalf("next 2027 #1: %v", err)
	}

	if first2026 != "PO-2026-0001" || second2026 != "PO-2026-0002" {
		t.Fatalf("2026 numbers = %q, %q; want PO-2026-0001, PO-2026-0002", first2026, second2026)
	}
	if first2027 != "PO-2027-0001" {
		t.Fatalf("2027 first number = %q, want PO-2027-0001", first2027)
	}
}

func TestRepositoryCreateEnforcesUniquePONumberPerBusiness(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 1, 1)

	err := db.Exec(
		"INSERT INTO purchases (business_id, supplier_id, po_number, total_amount) VALUES (?, ?, ?, ?)",
		businessID, fixture.supplierID, fixture.purchase.PONumber, 100,
	).Error
	if err == nil {
		t.Fatal("expected unique violation for a duplicate po_number, got nil")
	}
}

func TestListPaginatedFiltersByPONumber(t *testing.T) {
	db, businessID := setupTestDB(t)
	first := seedPurchase(t, db, businessID, 1, 1)
	seedPurchase(t, db, businessID, 1, 1)

	items, total, err := NewRepository(db).ListPaginated(
		businessID, 1, 20, nil, nil, nil, nil, &first.purchase.PONumber,
	)
	if err != nil {
		t.Fatalf("list by po_number: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total = %d, len = %d, want 1", total, len(items))
	}
	if items[0].PONumber != first.purchase.PONumber {
		t.Fatalf("po_number = %q, want %q", items[0].PONumber, first.purchase.PONumber)
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
		Status:      testStatusDraft,
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

func TestRepositoryCreateRejectsArchivedSupplier(t *testing.T) {
	db, businessID := setupTestDB(t)

	supplier := &suppliers.Supplier{BusinessID: businessID, Name: "Archived Supplier", PaymentTerms: suppliers.PaymentTermsNet30}
	if err := db.Create(supplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	if err := suppliers.NewRepository(db).Delete(supplier.ID, businessID); err != nil {
		t.Fatalf("archive supplier: %v", err)
	}

	product := &products.Product{BusinessID: businessID, Name: "Product A", Unit: "unit", Price: 300}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	purchase := &Purchase{BusinessID: businessID, SupplierID: supplier.ID, Status: testStatusDraft, TotalAmount: 100}
	items := []PurchaseLineItem{{ProductID: product.ID, Quantity: 1, UnitCost: 100, Subtotal: 100}}
	err := NewRepository(db).Create(purchase, items)
	if !errors.Is(err, suppliers.ErrSupplierNotFound) {
		t.Fatalf("error = %v, want %v", err, suppliers.ErrSupplierNotFound)
	}

	var count int64
	if err := db.Model(&Purchase{}).Where("supplier_id = ?", supplier.ID).Count(&count).Error; err != nil {
		t.Fatalf("count purchases: %v", err)
	}
	if count != 0 {
		t.Fatalf("purchases for archived supplier = %d, want 0", count)
	}
}

func TestRepositoryCreateRejectsSupplierFromAnotherBusiness(t *testing.T) {
	db, businessID := setupTestDB(t)

	otherBusiness := business.Business{ID: uuid.New(), Name: "Other Business"}
	if err := db.Create(&otherBusiness).Error; err != nil {
		t.Fatalf("create other business: %v", err)
	}
	supplier := &suppliers.Supplier{BusinessID: otherBusiness.ID, Name: "Foreign Supplier", PaymentTerms: suppliers.PaymentTermsNet30}
	if err := db.Create(supplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}

	product := &products.Product{BusinessID: businessID, Name: "Product A", Unit: "unit", Price: 300}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	purchase := &Purchase{BusinessID: businessID, SupplierID: supplier.ID, Status: testStatusDraft, TotalAmount: 100}
	items := []PurchaseLineItem{{ProductID: product.ID, Quantity: 1, UnitCost: 100, Subtotal: 100}}
	err := NewRepository(db).Create(purchase, items)
	if !errors.Is(err, suppliers.ErrSupplierNotFound) {
		t.Fatalf("error = %v, want %v", err, suppliers.ErrSupplierNotFound)
	}
}

func TestRepositoryUpdateDraftReplacesItemsAndTotals(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 2, 3)

	newSupplier := &suppliers.Supplier{BusinessID: businessID, Name: "Second Supplier", PaymentTerms: suppliers.PaymentTermsNet30}
	if err := db.Create(newSupplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	expectedAt := time.Date(2026, time.December, 2, 0, 0, 0, 0, time.UTC)
	items := []PurchaseLineItem{
		{ProductID: fixture.productA, Quantity: 5, UnitCost: 150, Subtotal: 750, Note: "rush"},
		{ProductID: fixture.productB, Quantity: 1, UnitCost: 400, Subtotal: 400},
	}

	updated, err := NewRepository(db).Update(fixture.purchase.ID, businessID, newSupplier.ID, 100, "updated note", &expectedAt, items)
	if err != nil {
		t.Fatalf("update purchase: %v", err)
	}
	if updated.TotalAmount != 1150 {
		t.Fatalf("total = %d, want 1150", updated.TotalAmount)
	}
	if updated.SupplierID != newSupplier.ID {
		t.Fatalf("supplier = %s, want %s", updated.SupplierID, newSupplier.ID)
	}
	if updated.Note != "updated note" {
		t.Fatalf("note = %q, want updated note", updated.Note)
	}
	if updated.PONumber != fixture.purchase.PONumber {
		t.Fatalf("po_number changed from %q to %q", fixture.purchase.PONumber, updated.PONumber)
	}
	if updated.ExpectedAt == nil || !updated.ExpectedAt.UTC().Equal(expectedAt) {
		t.Fatalf("expected_at = %v, want %v", updated.ExpectedAt, expectedAt)
	}
	if len(updated.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(updated.Items))
	}

	var count int64
	if err := db.Model(&PurchaseLineItem{}).Where("purchase_id = ?", fixture.purchase.ID).Count(&count).Error; err != nil {
		t.Fatalf("count items: %v", err)
	}
	if count != 2 {
		t.Fatalf("persisted line items = %d, want 2", count)
	}
}

func TestRepositoryUpdateRejectsPartiallyReceivedPurchase(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 5, 4)

	if _, err := receivePurchase(t, db, businessID, fixture.purchase.ID, []ReceiveItemInput{{ProductID: fixture.productA, ReceivedQty: 2}}); err != nil {
		t.Fatalf("partial receive: %v", err)
	}

	items := []PurchaseLineItem{{ProductID: fixture.productA, Quantity: 5, UnitCost: 100, Subtotal: 500}}
	_, err := NewRepository(db).Update(fixture.purchase.ID, businessID, fixture.supplierID, 0, "", nil, items)
	if !errors.Is(err, ErrPurchasePartiallyReceived) {
		t.Fatalf("error = %v, want %v", err, ErrPurchasePartiallyReceived)
	}
}

func TestRepositoryDeleteDraftRemovesPurchaseAndItems(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 2, 2)

	if err := NewRepository(db).Delete(fixture.purchase.ID, businessID); err != nil {
		t.Fatalf("delete purchase: %v", err)
	}
	if _, err := NewRepository(db).FindByID(fixture.purchase.ID, businessID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("find after delete error = %v, want record not found", err)
	}
	var count int64
	if err := db.Model(&PurchaseLineItem{}).Where("purchase_id = ?", fixture.purchase.ID).Count(&count).Error; err != nil {
		t.Fatalf("count items: %v", err)
	}
	if count != 0 {
		t.Fatalf("orphan line items = %d, want 0", count)
	}
}

func TestRepositoryDeleteRejectsReceivedPurchase(t *testing.T) {
	db, businessID := setupTestDB(t)
	fixture := seedPurchase(t, db, businessID, 1, 1)

	if _, err := receivePurchase(t, db, businessID, fixture.purchase.ID, nil); err != nil {
		t.Fatalf("receive all: %v", err)
	}
	err := NewRepository(db).Delete(fixture.purchase.ID, businessID)
	if !errors.Is(err, ErrAlreadyReceived) {
		t.Fatalf("error = %v, want %v", err, ErrAlreadyReceived)
	}
}
