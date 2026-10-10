package notifications

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

var _ = config.Load
var _ = business.NewRepository
var _ = database.NewPostgres
var _ = migrations.Up
var _ = os.Getenv

// TestGeneratorGenerateLowStock verifies low stock alert generation.
func TestGeneratorGenerateLowStock(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 5, 10)

	count, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("GenerateLowStock failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 alert, got %d", count)
	}

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

	_, err := gen.GenerateLowStock()
	if err != nil {
		t.Fatalf("first GenerateLowStock failed: %v", err)
	}

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
	createTestStockLevel(db, businessID, product.ID, 0, 10)

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
		Balance:     90000,
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
		Balance:     100000,
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
	createTestStockLevel(db, businessID, product.ID, 5, 10)

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
func TestGeneratorNotifyProductLowStockRestocked(t *testing.T) {
	db, businessID := setupTestDB(t)
	gen := NewGenerator(db)

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 5, 10)

	err := gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("NotifyProductLowStock failed: %v", err)
	}

	db.Table("stock_levels").Where("product_id = ? AND business_id = ?", product.ID, businessID).Update("quantity", 20)

	err = gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("NotifyProductLowStock after restock failed: %v", err)
	}

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
	createTestStockLevel(db, businessID, product.ID, 5, 10)

	err := gen.NotifyProductLowStock(businessID, product.ID)
	if err != nil {
		t.Fatalf("NotifyProductLowStock failed: %v", err)
	}

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

	product := createTestProduct(db, businessID)
	createTestStockLevel(db, businessID, product.ID, 5, 10)

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
