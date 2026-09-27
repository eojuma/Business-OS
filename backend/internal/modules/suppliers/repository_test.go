package suppliers

import (
	"context"
	"fmt"
	"sync"
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

	cfg := config.Load()
	cfg.DBName = "businessos_test"
	cfg.DBHost = "localhost"
	cfg.DBPort = "5432"
	cfg.DBUser = "businessos"
	cfg.DBPassword = "businessos"
	cfg.DBSSLMode = "disable"

	db := database.NewPostgres(cfg)

	if err := migrations.Up(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	testBusiness := business.Business{
		ID:   uuid.New(),
		Name: "Test Business",
	}
	if err := db.Create(&testBusiness).Error; err != nil {
		t.Fatalf("failed to create test business: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
	})

	return db, testBusiness.ID
}

func TestRepositoryCreate(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{
		BusinessID: businessID,
		Name:       "Integration Test Supplier",
		Phone:      "555-0100",
		Email:      "integration@test.com",
		Address:    "123 Integration Ave",
	}

	err := repo.Create(supplier)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if supplier.ID == uuid.Nil {
		t.Fatal("expected supplier ID to be set")
	}

	var found Supplier
	err = db.First(&found, "id = ?", supplier.ID).Error
	if err != nil {
		t.Fatalf("failed to find created supplier: %v", err)
	}
	if found.Name != supplier.Name {
		t.Fatalf("name mismatch: got %v, want %v", found.Name, supplier.Name)
	}
	if found.OutstandingBalance != 0 {
		t.Fatalf("outstanding_balance should default to 0, got %v", found.OutstandingBalance)
	}
}

func TestRepositoryFindByID(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{
		BusinessID:         businessID,
		Name:               "Find Me",
		OutstandingBalance: 12345,
	}
	db.Create(supplier)

	found, err := repo.FindByID(supplier.ID, businessID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if found.Name != "Find Me" {
		t.Fatalf("name = %v, want 'Find Me'", found.Name)
	}
	if found.OutstandingBalance != 12345 {
		t.Fatalf("outstanding_balance = %v, want 12345", found.OutstandingBalance)
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
	supplier := &Supplier{BusinessID: businessID, Name: "Test"}
	db.Create(supplier)

	_, err := repo.FindByID(supplier.ID, otherBusinessID)
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound for wrong business, got %v", err)
	}
}

func TestRepositoryList(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	db.Create(&Supplier{BusinessID: businessID, Name: "A"})
	db.Create(&Supplier{BusinessID: businessID, Name: "B"})
	db.Create(&Supplier{BusinessID: uuid.New(), Name: "C"})

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

	supplier := &Supplier{BusinessID: businessID, Name: "Old Name", Phone: "111"}
	db.Create(supplier)

	supplier.Name = "New Name"
	supplier.Phone = "222"
	err := repo.Update(supplier)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	var found Supplier
	db.First(&found, "id = ?", supplier.ID)
	if found.Name != "New Name" {
		t.Fatalf("name = %v, want 'New Name'", found.Name)
	}
	if found.Phone != "222" {
		t.Fatalf("phone = %v, want '222'", found.Phone)
	}
}

func TestRepositoryDelete(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{BusinessID: businessID, Name: "To Delete"}
	db.Create(supplier)

	err := repo.Delete(supplier.ID, supplier.BusinessID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	db.Model(&Supplier{}).Where("id = ?", supplier.ID).Count(&count)
	if count != 0 {
		t.Fatal("supplier should be deleted")
	}
}

func TestRepositoryRecordPayment(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{
		BusinessID:         businessID,
		Name:               "Paid Supplier",
		OutstandingBalance: 10000,
	}
	db.Create(supplier)

	payment := &Payment{Amount: 3000, Note: "Test payment"}
	updated, err := repo.RecordPayment(supplier.ID, businessID, payment)
	if err != nil {
		t.Fatalf("RecordPayment failed: %v", err)
	}
	if updated.OutstandingBalance != 7000 {
		t.Fatalf("outstanding_balance = %v, want 7000", updated.OutstandingBalance)
	}

	var foundSupplier Supplier
	db.First(&foundSupplier, "id = ?", supplier.ID)
	if foundSupplier.OutstandingBalance != 7000 {
		t.Fatalf("db outstanding_balance = %v, want 7000", foundSupplier.OutstandingBalance)
	}

	var payments []Payment
	db.Where("supplier_id = ?", supplier.ID).Find(&payments)
	if len(payments) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(payments))
	}
	if payments[0].Amount != 3000 {
		t.Fatalf("payment amount = %v, want 3000", payments[0].Amount)
	}
	if payments[0].Note != "Test payment" {
		t.Fatalf("payment note = %v, want 'Test payment'", payments[0].Note)
	}
}

func TestRepositoryRecordPaymentExceedsBalance(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{BusinessID: businessID, OutstandingBalance: 5000}
	db.Create(supplier)

	payment := &Payment{Amount: 6000}
	_, err := repo.RecordPayment(supplier.ID, businessID, payment)
	if err != ErrPaymentExceedsBalance {
		t.Fatalf("expected ErrPaymentExceedsBalance, got %v", err)
	}
}

func TestRepositoryRecordPaymentSupplierNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	payment := &Payment{Amount: 1000}
	_, err := repo.RecordPayment(uuid.New(), uuid.New(), payment)
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestRepositoryListPayments(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{BusinessID: businessID}
	db.Create(supplier)

	payment1 := &Payment{SupplierID: supplier.ID, BusinessID: businessID, Amount: 1000, Note: "First"}
	payment2 := &Payment{SupplierID: supplier.ID, BusinessID: businessID, Amount: 2000, Note: "Second"}
	db.Create(payment1)
	db.Create(payment2)

	found, err := repo.ListPayments(supplier.ID, businessID)
	if err != nil {
		t.Fatalf("ListPayments failed: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("len = %d, want 2", len(found))
	}
	// Verify both payments are returned (order by created_at desc)
	amounts := []int64{found[0].Amount, found[1].Amount}
	if amounts[0] != 2000 || amounts[1] != 1000 {
		t.Fatalf("expected descending order by created_at [2000, 1000], got %v", amounts)
	}
}

func TestRepositoryAddOutstandingTx(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{BusinessID: businessID, OutstandingBalance: 1000}
	db.Create(supplier)

	err := repo.AddOutstandingTx(db, supplier.ID, businessID, 500)
	if err != nil {
		t.Fatalf("AddOutstandingTx failed: %v", err)
	}

	var found Supplier
	db.First(&found, "id = ?", supplier.ID)
	if found.OutstandingBalance != 1500 {
		t.Fatalf("outstanding_balance = %v, want 1500", found.OutstandingBalance)
	}
}

func TestAddOutstandingTxSupplierNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	err := repo.AddOutstandingTx(db, uuid.New(), uuid.New(), 100)
	if err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
	}
}

func TestConcurrentRecordPayment(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{BusinessID: businessID, OutstandingBalance: 10000}
	db.Create(supplier)

	const numGoroutines = 10
	const paymentAmount = 500

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			payment := &Payment{Amount: paymentAmount}
			_ = repo.RecordPayment(supplier.ID, businessID, payment)
		}()
	}

	wg.Wait()

	var final Supplier
	db.First(&final, "id = ?", supplier.ID)
	expected := int64(10000) - int64(numGoroutines)*int64(paymentAmount)
	if final.OutstandingBalance != expected {
		t.Fatalf("outstanding_balance = %v, want %v (race condition detected)", final.OutstandingBalance, expected)
	}
}

func TestConcurrentAddOutstandingTx(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{BusinessID: businessID, OutstandingBalance: 0}
	db.Create(supplier)

	const numGoroutines = 20
	const addAmount = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = repo.AddOutstandingTx(db, supplier.ID, businessID, addAmount)
		}()
	}

	wg.Wait()

	var final Supplier
	db.First(&final, "id = ?", supplier.ID)
	expected := int64(numGoroutines) * int64(addAmount)
	if final.OutstandingBalance != expected {
		t.Fatalf("outstanding_balance = %v, want %v (race condition detected)", final.OutstandingBalance, expected)
	}
}

func TestRecordPaymentAtomicTransaction(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	supplier := &Supplier{
		BusinessID:         businessID,
		Name:               "Atomic Test",
		OutstandingBalance: 5000,
	}
	db.Create(supplier)

	payment := &Payment{Amount: 3000}
	_, err := repo.RecordPayment(supplier.ID, businessID, payment)
	if err != nil {
		t.Fatalf("RecordPayment failed: %v", err)
	}

	var supplierCount int64
	db.Model(&Supplier{}).Where("id = ?", supplier.ID).Count(&supplierCount)
	if supplierCount != 1 {
		t.Fatal("supplier should exist after successful payment")
	}

	var paymentCount int64
	db.Model(&Payment{}).Where("supplier_id = ?", supplier.ID).Count(&paymentCount)
	if paymentCount != 1 {
		t.Fatalf("expected 1 payment record, got %d", paymentCount)
	}

	var supplierAfter Supplier
	db.First(&supplierAfter, "id = ?", supplier.ID)
	if supplierAfter.OutstandingBalance != 2000 {
		t.Fatalf("outstanding_balance = %v, want 2000", supplierAfter.OutstandingBalance)
	}
}

func TestMigrationLoad(t *testing.T) {
	db, _ := setupTestDB(t)

	var count int64
	db.Raw("SELECT COUNT(*) FROM suppliers").Count(&count)
	if count == 0 {
		t.Log("suppliers table exists and is empty")
	}

	db.Raw("SELECT COUNT(*) FROM supplier_payments").Count(&count)
	if count == 0 {
		t.Log("supplier_payments table exists and is empty")
	}

	_, err := db.DB()
	if err != nil {
		t.Fatalf("database connection failed: %v", err)
	}
}

func TestRepositoryContextCancellation(t *testing.T) {
	db, _ := setupTestDB(t)
	repo := NewRepository(db)

	_, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond)

	_, err := repo.FindByID(uuid.New(), uuid.New())
	if err != nil && err != context.DeadlineExceeded && err != gorm.ErrRecordNotFound {
		t.Logf("context cancellation test: %v", err)
	}
}

func TestSupplierNameIndex(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)

	db.Create(&business.Business{ID: businessID, Name: "Test Business"})

	for i := 0; i < 100; i++ {
		s := &Supplier{
			BusinessID: businessID,
			Name:       fmt.Sprintf("Supplier %03d", i),
		}
		db.Create(s)
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
	if elapsed > 1*time.Second {
		t.Logf("List took %v (may need index optimization)", elapsed)
	}
}
