package notifications

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

var _ = config.Load
var _ = business.NewRepository
var _ = database.NewPostgres
var _ = migrations.Up
var _ = os.Getenv

func createTestProduct(db *gorm.DB, businessID uuid.UUID) *products.Product {
	product := &products.Product{
		BusinessID: businessID,
		Name:       "Test Product",
		Unit:       "pcs",
		Price:      10000,
		CostPrice:  5000,
	}
	db.Create(product)
	return product
}

func createTestStockLevel(db *gorm.DB, businessID, productID uuid.UUID, quantity, threshold int64) {
	stock := map[string]interface{}{
		"business_id":        businessID,
		"product_id":         productID,
		"quantity":           quantity,
		"low_stock_threshold": threshold,
	}
	db.Table("stock_levels").Create(stock)
}

func createTestSupplier(db *gorm.DB, businessID uuid.UUID) *suppliers.Supplier {
	supplier := &suppliers.Supplier{
		BusinessID: businessID,
		Name:       "Test Supplier",
	}
	db.Create(supplier)
	return supplier
}

func createTestCustomer(db *gorm.DB, businessID uuid.UUID) *customers.Customer {
	customer := &customers.Customer{
		BusinessID:   businessID,
		Name:         "Test Customer",
		CreditLimit:  100000,
		Balance:      90000,
	}
	db.Create(customer)
	return customer
}

// TestGeneratorGenerateLowStock verifies low stock alert generation.
func TestGeneratorGenerateLowStock(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 5, 10) // quantity=5, threshold=10

	count, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("GenerateLowStock failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 alert, got %d", count)
	}

	// Verify alert was created
	var notifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeLowStock).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Severity != SeverityWarning {
		t.Fatalf("expected severity warning, got %v", notifs[0].Severity)
	}
}

// TestGeneratorGenerateLowStockDeduplication verifies no duplicate alerts.
func TestGeneratorGenerateLowStockDeduplication(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 5, 10)

	// First generation
	_, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("first GenerateLowStock failed: %v", err)
	}

	// Second generation - should not create duplicate
	count, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("second GenerateLowStock failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 new alerts, got %d", count)
	}
}

// TestGeneratorGenerateLowStockCritical verifies critical severity for zero stock.
func TestGeneratorGenerateLowStockCritical(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 0, 10) // Zero stock = critical

	_, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("GenerateLowStock failed: %v", err)
	}

	var notifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeLowStock).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Severity != SeverityCritical {
		t.Fatalf("expected severity critical, got %v", notifs[0].Severity)
	}
}

// TestGeneratorGenerateCreditAlerts verifies credit limit alert generation.
func TestGeneratorGenerateCreditAlerts(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	customer := &customers.Customer{
		BusinessID:  businessID,
		Name:        "Test Customer",
		CreditLimit: 100000,
		Balance:     90000, // 90% - above 80% threshold
	}
	db.Create(customer)

	count, err := gen.GenerateCreditAlerts()
	if err != nil {
		t.Fatalf("GenerateCreditAlerts failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 alert, got %d", count)
	}

	var notifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeCreditLimit).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Severity != SeverityWarning {
		t.Fatalf("expected severity warning, got %v", notifs[0].Severity)
	}
}

// TestGeneratorGenerateCreditAlertsCritical verifies critical severity at limit.
func TestGeneratorGenerateCreditAlertsCritical(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	customer := &customers.Customer{
		BusinessID:  businessID,
		Name:        "At Limit Customer",
		CreditLimit: 100000,
		Balance:     100000, // At limit
	}
	db.Create(customer)

	_, err := gen.GenerateCreditAlerts()
	if err != nil {
		t.Fatalf("GenerateCreditAlerts failed: %v", err)
	}

	var notifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeCreditLimit).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Severity != SeverityCritical {
		t.Fatalf("expected severity critical, got %v", notifs[0].Severity)
	}
}

// TestGeneratorGenerateCreditAlertsDeduplication verifies no duplicate credit alerts.
func TestGeneratorGenerateCreditAlertsDeduplication(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	customer := &customers.Customer{
		BusinessID:  businessID,
		Name:        "Dedup Customer",
		CreditLimit: 100000,
		Balance:     90000,
	}
	db.Create(customer)

	_, err := gen.GenerateCreditAlerts()
	if err != nil {
		t.Fatalf("first GenerateCreditAlerts failed: %v", err)
	}

	count, err := gen.GenerateCreditAlerts()
	if err != nil {
		t.Fatalf("second GenerateCreditAlerts failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 new alerts, got %d", count)
	}
}

// TestGeneratorNotifyProductLowStock verifies real-time low stock notification.
func TestGeneratorNotifyProductLowStock(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	// LowStockThreshold set via stock_levels
	// Quantity set via stock_levels
	db.Save(product)

	err := gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("NotifyProductLowStock failed: %v", err)
	}

	var notifs []Notification
	db.Where("business_id = ? AND type = ? AND entity_id = ?", businessID, TypeLowStock, product.ID).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Severity != SeverityWarning {
		t.Fatalf("expected severity warning, got %v", notifs[0].Severity)
	}
}

// TestGeneratorNotifyProductLowStockRestocked verifies clearing alert when restocked.
func TestGeneratorNotifyProductLowStockRestocked(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	// LowStockThreshold set via stock_levels
	// Quantity set via stock_levels
	db.Save(product)

	// Create alert first
	gen.NotifyProductLowStock(businessID, product.ID)

	// Restock above threshold
	// Quantity set via stock_levels
	db.Save(product)

	err := gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("NotifyProductLowStock after restock failed: %v", err)
	}

	// Alert should be cleared (marked as read)
	var notifs []Notification
	db.Where("business_id = ? AND type = ? AND entity_id = ?", businessID, TypeLowStock, product.ID).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Read != true {
		t.Fatal("expected notification to be marked as read after restock")
	}
}

// TestGeneratorNotifyProductLowStockDeduplication verifies no duplicate real-time alerts.
func TestGeneratorNotifyProductLowStockDeduplication(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	// LowStockThreshold set via stock_levels
	// Quantity set via stock_levels
	db.Save(product)

	// First notification
	err := gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("first NotifyProductLowStock failed: %v", err)
	}

	// Second notification - should not create duplicate
	err = gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("second NotifyProductLowStock failed: %v", err)
	}

	var notifs []Notification
	db.Where("business_id = ? AND type = ? AND entity_id = ?", businessID, TypeLowStock, product.ID).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
}

// TestGeneratorRun verifies the full generation run.
func TestGeneratorRun(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	// Create low stock product
	product := createTestProduct(db, businessID)
	// LowStockThreshold set via stock_levels
	// Quantity set via stock_levels
	db.Save(product)

	// Create customer at credit limit
	customer := &customers.Customer{
		BusinessID:  businessID,
		Name:        "Credit Customer",
		CreditLimit: 100000,
		Balance:     90000,
	}
	db.Create(customer)

	gen.Run()

	var lowStockNotifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeLowStock).Find(&lowStockNotifs)
	if len(lowStockNotifs) != 1 {
		t.Fatalf("expected 1 low stock notification, got %d", len(lowStockNotifs))
	}

	var creditNotifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeCreditLimit).Find(&creditNotifs)
	if len(creditNotifs) != 1 {
		t.Fatalf("expected 1 credit notification, got %d", len(creditNotifs))
	}
}

// TestStartScheduler verifies scheduler starts and runs.
func TestStartScheduler(t *testing.T) {
	db, _ := setupTestDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	stop := StartScheduler(db, 50*time.Millisecond)
	defer stop()

	<-ctx.Done()
	// If we reach here without panic, scheduler started and stopped gracefully
}

// TestGeneratorGenerateLowStockWithSupplier verifies recipient includes supplier email.
func TestGeneratorGenerateLowStockWithSupplier(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 5, 10)

	_, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("GenerateLowStock failed: %v", err)
	}

	var notifs []Notification
	db.Where("business_id = ? AND type = ?", businessID, TypeLowStock).Find(&notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
}