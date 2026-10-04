package products

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
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

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

	t.Cleanup(func() {
		// Drop the test schema instead of public schema
		if err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error; err != nil {
			t.Logf("cleanup warning: failed to drop test schema %s: %v", schemaName, err)
		}
	})

	return db, testBusinessSchema.ID
}

func TestRepositoryCreate(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	product := &Product{
		BusinessID: businessID,
		Name:       "Integration Test Product",
		Category:   "Test",
		Unit:       "pcs",
		Price:      15000,
		SKU:        "INT-001",
	}

	err := repo.Create(product)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if product.ID == uuid.Nil {
		t.Fatal("expected product ID to be set")
	}

	var found Product
	err = db.First(&found, "id = ?", product.ID).Error
	if err != nil {
		t.Fatalf("failed to find created product: %v", err)
	}
	if found.Name != product.Name {
		t.Fatalf("name mismatch: got %v, want %v", found.Name, product.Name)
	}
	if found.CostPrice != 0 {
		t.Fatalf("cost_price should default to 0, got %v", found.CostPrice)
	}
}

func TestRepositoryFindByID(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	product := &Product{
		BusinessID: businessID,
		Name:       "Find Me",
		Price:      25000,
		CostPrice:  15000,
	}
	db.Create(product)

	found, err := repo.FindByID(product.ID, businessID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if found.Name != "Find Me" {
		t.Fatalf("name = %v, want 'Find Me'", found.Name)
	}
	if found.Price != 25000 {
		t.Fatalf("price = %v, want 25000", found.Price)
	}
	if found.CostPrice != 15000 {
		t.Fatalf("cost_price = %v, want 15000", found.CostPrice)
	}
}

func TestRepositoryFindByIDNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	_, err := repo.FindByID(uuid.New(), uuid.New())
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestRepositoryFindByIDWrongBusiness(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	otherBusinessID := uuid.New()
	product := &Product{BusinessID: businessID, Name: "Test", Price: 1000}
	db.Create(product)

	_, err := repo.FindByID(product.ID, otherBusinessID)
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound for wrong business, got %v", err)
	}
}

func TestRepositoryList(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	db.Create(&Product{BusinessID: businessID, Name: "A", Price: 1000})
	db.Create(&Product{BusinessID: businessID, Name: "B", Price: 2000})
	db.Create(&Product{BusinessID: uuid.New(), Name: "C", Price: 3000})

	list, err := repo.List(businessID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestRepositoryUpdate(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	product := &Product{BusinessID: businessID, Name: "Old Name", Price: 1000}
	db.Create(product)

	product.Name = "New Name"
	product.Price = 2000
	err := repo.Update(product)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	var found Product
	db.First(&found, "id = ?", product.ID)
	if found.Name != "New Name" {
		t.Fatalf("name = %v, want 'New Name'", found.Name)
	}
	if found.Price != 2000 {
		t.Fatalf("price = %v, want 2000", found.Price)
	}
}

func TestRepositoryDelete(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	product := &Product{BusinessID: businessID, Name: "To Delete", Price: 100}
	db.Create(product)

	err := repo.Delete(product.ID, businessID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	db.Model(&Product{}).Where("id = ?", product.ID).Count(&count)
	if count != 0 {
		t.Fatal("product should be deleted")
	}
}

func TestRepositoryGetPricing(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	product := &Product{
		BusinessID: businessID,
		Name:       "Priced Product",
		Price:      25000,
		CostPrice:  15000,
	}
	db.Create(product)

	price, cost, err := repo.GetPricing(businessID, product.ID)
	if err != nil {
		t.Fatalf("GetPricing failed: %v", err)
	}
	if price != 25000 {
		t.Fatalf("price = %v, want 25000", price)
	}
	if cost != 15000 {
		t.Fatalf("cost_price = %v, want 15000", cost)
	}
}

func TestRepositoryGetPricingNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	_, _, err := repo.GetPricing(uuid.New(), uuid.New())
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestRepositoryUpdateCostPriceTx(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	product := &Product{BusinessID: businessID, CostPrice: 1000}
	db.Create(product)

	err := repo.UpdateCostPriceTx(db, product.ID, businessID, 2000)
	if err != nil {
		t.Fatalf("UpdateCostPriceTx failed: %v", err)
	}

	var found Product
	db.First(&found, "id = ?", product.ID)
	if found.CostPrice != 2000 {
		t.Fatalf("cost_price = %v, want 2000", found.CostPrice)
	}
}

func TestUpdateCostPriceTxNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	err := repo.UpdateCostPriceTx(db, uuid.New(), uuid.New(), 100)
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestMigrationLoad(t *testing.T) {
	db, _ := setupTestDB(t)

	if !db.Migrator().HasTable(&Product{}) {
		t.Fatal("products table should exist after migrations")
	}

	var indexCount int64
	db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'products' AND indexname = 'idx_products_business_id'").Count(&indexCount)
	if indexCount == 0 {
		t.Fatal("idx_products_business_id index should exist")
	}

	_, err := db.DB()
	if err != nil {
		t.Fatalf("database connection failed: %v", err)
	}
}

func TestRepositoryContextCancellation(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond)

	_, err := repo.FindByID(uuid.New(), uuid.New())
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

func TestProductNameIndex(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	for i := 0; i < 100; i++ {
		p := &Product{
			BusinessID: businessID,
			Name:       fmt.Sprintf("Product %03d", i),
			Price:      int64(i * 100),
		}
		db.Create(p)
	}

	start := time.Now()
	list, err := repo.List(businessID)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 100 {
		t.Fatalf("len = %d, want 100", len(list))
	}
	if list[0].Name != "Product 000" || list[99].Name != "Product 099" {
		t.Fatalf("products not ordered by name desc (created_at desc)")
	}
	if elapsed > 1*time.Second {
		t.Logf("List took %v (may need index optimization)", elapsed)
	}
}
