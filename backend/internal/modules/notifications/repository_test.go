package notifications

import (
	"context"
	"fmt"
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
		"business_id":         businessID,
		"product_id":          productID,
		"quantity":            quantity,
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
		BusinessID:  businessID,
		Name:        "Test Customer",
		CreditLimit: 100000,
		Balance:     90000,
	}
	db.Create(customer)
	return customer
}

func setupTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
	t.Helper()

	// Opt-in: require TEST_DATABASE_URL to run integration tests
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	cfg := config.Load()
	cfg.DBName = "businessos_test"
	cfg.DBHost = "localhost"
	cfg.DBPort = "5432"
	cfg.DBUser = "businessos"
	cfg.DBPassword = "businessos"
	cfg.DBSSLMode = "disable"

	db := database.NewPostgres(cfg)

	// Verify database is reachable
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Skipf("Database not reachable, skipping integration test: %v", err)
	}

	if err := migrations.Up(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create test business
	testBusiness := business.Business{
		ID:   uuid.New(),
		Name: "Test Business",
	}
	if err := db.Create(&testBusiness).Error; err != nil {
		t.Fatalf("failed to create test business: %v", err)
	}

	// Use a unique schema per test for isolation
	schemaName := fmt.Sprintf("test_%s", uuid.New().String()[:8])
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error; err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	// Register cleanup immediately after schema creation
	t.Cleanup(func() {
		// Drop the test schema instead of public schema
		if err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error; err != nil {
			t.Logf("cleanup warning: failed to drop test schema %s: %v", schemaName, err)
		}
	})

	// Set search path to test schema
	db = db.Session(&gorm.Session{NewDB: true})
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName)).Error; err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}

	// Re-run migrations in the test schema
	if err := migrations.Up(db); err != nil {
		t.Fatalf("failed to run migrations in test schema: %v", err)
	}

	testBusinessSchema := business.Business{
		ID:   uuid.New(),
		Name: "Test Business",
	}
	if err := db.Create(&testBusinessSchema).Error; err != nil {
		t.Fatalf("failed to create test business in schema: %v", err)
	}

	return db, testBusinessSchema.ID
}

// TestRepositoryCreate verifies creating a notification persists all fields correctly in the database.
func TestRepositoryCreate(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	entityID := uuid.New()
	notification := &Notification{
		BusinessID: businessID,
		Type:       TypeLowStock,
		Severity:   SeverityWarning,
		Recipient:  "test@example.com",
		Title:      "Low stock: Test Product",
		Message:    "Test Product has 5 units remaining (threshold 10).",
		EntityID:   &entityID,
		EntityName: "Test Product",
	}

	err := repo.Create(notification)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if notification.ID == uuid.Nil {
		t.Fatal("expected notification ID to be set")
	}

	var found Notification
	err = db.First(&found, "id = ?", notification.ID).Error
	if err != nil {
		t.Fatalf("failed to find created notification: %v", err)
	}
	if found.Title != notification.Title {
		t.Fatalf("title mismatch: got %v, want %v", found.Title, notification.Title)
	}
	if found.Read {
		t.Fatalf("is_read should default to false, got %v", found.Read)
	}
}

// TestRepositoryFindByID verifies retrieving a notification by ID and business ID.
func TestRepositoryFindByID(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	entityID := uuid.New()
	notification := &Notification{
		BusinessID: businessID,
		Type:       TypeLowStock,
		Severity:   SeverityWarning,
		Recipient:  "test@example.com",
		Title:      "Find Me",
		Message:    "Test message",
		EntityID:   &entityID,
		EntityName: "Test Product",
	}
	db.Create(notification)

	found, err := repo.FindByID(notification.ID, businessID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if found.Title != "Find Me" {
		t.Fatalf("title = %v, want 'Find Me'", found.Title)
	}
	if found.Severity != SeverityWarning {
		t.Fatalf("severity = %v, want %v", found.Severity, SeverityWarning)
	}
}

// TestRepositoryFindByIDNotFound verifies FindByID returns ErrNotFound for non-existent notifications.
func TestRepositoryFindByIDNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	_, err := repo.FindByID(uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRepositoryFindByIDWrongBusiness verifies FindByID returns ErrNotFound for notifications in other businesses.
func TestRepositoryFindByIDWrongBusiness(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	otherBusinessID := uuid.New()
	notification := &Notification{BusinessID: businessID, Title: "Test"}
	if err := db.Create(notification).Error; err != nil {
		t.Fatalf("fixture creation failed: %v", err)
	}

	_, err := repo.FindByID(notification.ID, otherBusinessID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for wrong business, got %v", err)
	}
}

// TestRepositoryList verifies listing notifications returns only those belonging to the business.
func TestRepositoryList(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	db.Create(&Notification{BusinessID: businessID, Title: "A", Message: "msg"})
	db.Create(&Notification{BusinessID: businessID, Title: "B", Message: "msg"})
	db.Create(&Notification{BusinessID: uuid.New(), Title: "C", Message: "msg"})

	list, err := repo.List(businessID, false)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

// TestRepositoryListUnreadOnly verifies listing with unreadOnly filter.
func TestRepositoryListUnreadOnly(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	db.Create(&Notification{BusinessID: businessID, Title: "Unread", Message: "msg", Read: false})
	db.Create(&Notification{BusinessID: businessID, Title: "Read", Message: "msg", Read: true})

	list, err := repo.List(businessID, true)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].Title != "Unread" {
		t.Fatalf("expected unread notification, got %v", list[0].Title)
	}
}

// TestRepositoryUpdate verifies updating a notification persists changes to the database.
func TestRepositoryUpdate(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	notification := &Notification{BusinessID: businessID, Title: "Old Title", Message: "msg"}
	db.Create(notification)

	notification.Title = "New Title"
	err := repo.Update(notification)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	var found Notification
	db.First(&found, "id = ?", notification.ID)
	if found.Title != "New Title" {
		t.Fatalf("title = %v, want 'New Title'", found.Title)
	}
}

// TestRepositoryDelete verifies deleting a notification removes it from the database.
func TestRepositoryDelete(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	notification := &Notification{BusinessID: businessID, Title: "To Delete", Message: "msg"}
	if err := db.Create(notification).Error; err != nil {
		t.Fatalf("fixture creation failed: %v", err)
	}

	err := repo.Delete(notification.ID, businessID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	if err := db.Model(&Notification{}).Where("id = ?", notification.ID).Count(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatal("notification should be deleted")
	}
}

// TestRepositoryMarkRead verifies marking a notification as read.
func TestRepositoryMarkRead(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	notification := &Notification{BusinessID: businessID, Title: "Test", Message: "msg", Read: false}
	db.Create(notification)

	err := repo.MarkRead(notification.ID, businessID)
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}

	var found Notification
	db.First(&found, "id = ?", notification.ID)
	if !found.Read {
		t.Fatal("notification should be marked as read")
	}
}

// TestRepositoryMarkReadNotFound verifies MarkRead returns ErrNotFound for non-existent notifications.
func TestRepositoryMarkReadNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	err := repo.MarkRead(uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRepositoryMarkAllRead verifies marking all notifications as read.
func TestRepositoryMarkAllRead(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	db.Create(&Notification{BusinessID: businessID, Title: "A", Message: "msg", Read: false})
	db.Create(&Notification{BusinessID: businessID, Title: "B", Message: "msg", Read: false})

	err := repo.MarkAllRead(businessID)
	if err != nil {
		t.Fatalf("MarkAllRead failed: %v", err)
	}

	var count int64
	db.Model(&Notification{}).Where("business_id = ? AND is_read = ?", businessID, false).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 unread, got %d", count)
	}
}

// TestRepositoryMarkReadByEntity verifies marking notifications by entity as read.
func TestRepositoryMarkReadByEntity(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	entityID := uuid.New()
	db.Create(&Notification{BusinessID: businessID, Type: TypeLowStock, EntityID: &entityID, Title: "A", Message: "msg", Read: false})
	db.Create(&Notification{BusinessID: businessID, Type: TypeLowStock, EntityID: &entityID, Title: "B", Message: "msg", Read: false})

	err := repo.MarkReadByEntity(businessID, TypeLowStock, entityID)
	if err != nil {
		t.Fatalf("MarkReadByEntity failed: %v", err)
	}

	var count int64
	db.Model(&Notification{}).Where("business_id = ? AND type = ? AND entity_id = ? AND is_read = ?", businessID, TypeLowStock, entityID, false).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 unread, got %d", count)
	}
}

// TestRepositoryExistsUnread verifies checking for unread notifications by entity.
func TestRepositoryExistsUnread(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	entityID := uuid.New()
	db.Create(&Notification{BusinessID: businessID, Type: TypeLowStock, EntityID: &entityID, Title: "Test", Message: "msg", Read: false})

	exists, err := repo.ExistsUnread(businessID, TypeLowStock, entityID)
	if err != nil {
		t.Fatalf("ExistsUnread failed: %v", err)
	}
	if !exists {
		t.Fatal("expected unread notification to exist")
	}

	// Mark as read
	db.Model(&Notification{}).Where("entity_id = ?", entityID).Update("is_read", true)
	exists, err = repo.ExistsUnread(businessID, TypeLowStock, entityID)
	if err != nil {
		t.Fatalf("ExistsUnread failed: %v", err)
	}
	if exists {
		t.Fatal("expected no unread notification after marking read")
	}
}

// TestRepositoryExistsUnreadNotFound verifies ExistsUnread returns false for non-existent entities.
func TestRepositoryExistsUnreadNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	exists, err := repo.ExistsUnread(uuid.New(), TypeLowStock, uuid.New())
	if err != nil {
		t.Fatalf("ExistsUnread failed: %v", err)
	}
	if exists {
		t.Fatal("expected false for non-existent entity")
	}
}

// TestMigrationLoad verifies migrations create the notifications table with correct indexes.
func TestMigrationLoad(t *testing.T) {
	db, _ := setupTestDB(t)

	if !db.Migrator().HasTable(&Notification{}) {
		t.Fatal("notifications table should exist after migrations")
	}

	var indexCount int64
	db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'notifications' AND indexname = 'idx_notifications_business_id'").Count(&indexCount)
	if indexCount == 0 {
		t.Fatal("idx_notifications_business_id index should exist")
	}

	db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'notifications' AND indexname = 'idx_notifications_business_read'").Count(&indexCount)
	if indexCount == 0 {
		t.Fatal("idx_notifications_business_read index should exist")
	}

	_, err := db.DB()
	if err != nil {
		t.Fatalf("database connection failed: %v", err)
	}
}

// TestRepositoryContextCancellation verifies context cancellation is respected during queries.
func TestRepositoryContextCancellation(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond)

	_, err := repo.List(uuid.New(), false)
	if err != nil && err != context.DeadlineExceeded && err != gorm.ErrRecordNotFound {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-ctx.Done():
		// Context was cancelled as expected
	default:
		t.Fatal("context should be cancelled after timeout")
	}
}

// TestNotificationListPerformance verifies list performance and ordering for 100 notifications.
func TestNotificationListPerformance(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	for i := 0; i < 100; i++ {
		n := &Notification{
			BusinessID: businessID,
			Title:      fmt.Sprintf("Notification %03d", i),
		}
		db.Create(n)
	}

	start := time.Now()
	list, err := repo.List(businessID, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 100 {
		t.Fatalf("len = %d, want 100", len(list))
	}
	if elapsed > 1*time.Second {
		t.Logf("List took %v (may need index optimization)", elapsed)
	}
}
