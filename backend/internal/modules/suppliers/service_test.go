package suppliers

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type testRepository struct {
	suppliers map[uuid.UUID]*Supplier
	payments  map[uuid.UUID][]Payment
}

func newTestRepository() *testRepository {
	return &testRepository{
		suppliers: make(map[uuid.UUID]*Supplier),
		payments:  make(map[uuid.UUID][]Payment),
	}
}

func (r *testRepository) Create(s *Supplier) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	r.suppliers[s.ID] = s
	return nil
}

func (r *testRepository) FindByID(id, businessID uuid.UUID) (*Supplier, error) {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return s, nil
}

func (r *testRepository) List(businessID uuid.UUID) ([]Supplier, error) {
	var result []Supplier
	for _, s := range r.suppliers {
		if s.BusinessID == businessID {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (r *testRepository) Update(s *Supplier) error {
	if _, ok := r.suppliers[s.ID]; !ok {
		return gorm.ErrRecordNotFound
	}
	r.suppliers[s.ID] = s
	return nil
}

func (r *testRepository) Delete(id, businessID uuid.UUID) error {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return gorm.ErrRecordNotFound
	}
	delete(r.suppliers, id)
	return nil
}

func (r *testRepository) RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Supplier, error) {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	if payment.Amount > s.OutstandingBalance {
		return nil, ErrPaymentExceedsBalance
	}
	s.OutstandingBalance -= payment.Amount
	payment.BusinessID, payment.SupplierID = businessID, id
	if payment.ID == uuid.Nil {
		payment.ID = uuid.New()
	}
	r.payments[id] = append(r.payments[id], *payment)
	return s, nil
}

func (r *testRepository) ListPayments(id, businessID uuid.UUID) ([]Payment, error) {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return r.payments[id], nil
}

func (r *testRepository) AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return ErrSupplierNotFound
	}
	s.OutstandingBalance += amount
	return nil
}

func TestCreateSupplier(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	input := CreateInput{
		BusinessID: businessID,
		Name:       "Test Supplier",
		Phone:      "1234567890",
		Email:      "test@supplier.com",
		Address:    "123 Test St",
	}

	supplier, err := svc.Create(input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if supplier.ID == uuid.Nil {
		t.Fatal("expected supplier ID to be set")
	}
	if supplier.BusinessID != businessID {
		t.Fatalf("business_id = %v, want %v", supplier.BusinessID, businessID)
	}
	if supplier.Name != "Test Supplier" {
		t.Fatalf("name = %v, want 'Test Supplier'", supplier.Name)
	}
	if supplier.OutstandingBalance != 0 {
		t.Fatalf("outstanding_balance = %v, want 0", supplier.OutstandingBalance)
	}
}

func TestGetSupplier(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Test", OutstandingBalance: 5000}
	repo.suppliers[supplier.ID] = supplier

	got, err := svc.Get(supplier.ID, businessID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "Test" {
		t.Fatalf("name = %v, want 'Test'", got.Name)
	}
	if got.OutstandingBalance != 5000 {
		t.Fatalf("outstanding_balance = %v, want 5000", got.OutstandingBalance)
	}
}

func TestGetSupplierNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.Get(uuid.New(), uuid.New())
	if err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
	}
}

func TestListSuppliers(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "A"}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "B"}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: uuid.New(), Name: "C"}

	list, err := svc.List(businessID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestUpdateSupplier(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Old", Phone: "111"}
	repo.suppliers[supplier.ID] = supplier

	newName := "New Name"
	newPhone := "2222222222"
	updated, err := svc.Update(supplier.ID, businessID, UpdateInput{
		Name:  &newName,
		Phone: &newPhone,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("name = %v, want 'New Name'", updated.Name)
	}
	if updated.Phone != "2222222222" {
		t.Fatalf("phone = %v, want '2222222222'", updated.Phone)
	}
}

func TestUpdateSupplierNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.Update(uuid.New(), uuid.New(), UpdateInput{})
	if err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
	}
}

func TestDeleteSupplier(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID}
	repo.suppliers[supplier.ID] = supplier

	err := svc.Delete(supplier.ID, businessID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, ok := repo.suppliers[supplier.ID]; ok {
		t.Fatal("supplier should be deleted")
	}
}

func TestRecordPaymentValid(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Test", OutstandingBalance: 10000}
	repo.suppliers[supplier.ID] = supplier

	updated, err := svc.RecordPayment(supplier.ID, businessID, 3000, "Partial payment")
	if err != nil {
		t.Fatalf("RecordPayment failed: %v", err)
	}
	if updated.OutstandingBalance != 7000 {
		t.Fatalf("outstanding_balance = %v, want 7000", updated.OutstandingBalance)
	}
	if len(repo.payments[supplier.ID]) != 1 {
		t.Fatal("payment should be recorded")
	}
	if repo.payments[supplier.ID][0].Amount != 3000 {
		t.Fatalf("payment amount = %v, want 3000", repo.payments[supplier.ID][0].Amount)
	}
	if repo.payments[supplier.ID][0].Note != "Partial payment" {
		t.Fatalf("payment note = %v, want 'Partial payment'", repo.payments[supplier.ID][0].Note)
	}
}

func TestRecordPaymentInvalidAmount(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, OutstandingBalance: 10000}
	repo.suppliers[supplier.ID] = supplier

	_, err := svc.RecordPayment(supplier.ID, businessID, 0, "")
	if err != ErrInvalidPayment {
		t.Fatalf("expected ErrInvalidPayment, got %v", err)
	}

	_, err = svc.RecordPayment(supplier.ID, businessID, -100, "")
	if err != ErrInvalidPayment {
		t.Fatalf("expected ErrInvalidPayment, got %v", err)
	}
}

func TestRecordPaymentExceedsBalance(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, OutstandingBalance: 5000}
	repo.suppliers[supplier.ID] = supplier

	_, err := svc.RecordPayment(supplier.ID, businessID, 6000, "")
	if err != ErrPaymentExceedsBalance {
		t.Fatalf("expected ErrPaymentExceedsBalance, got %v", err)
	}
}

func TestRecordPaymentSupplierNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.RecordPayment(uuid.New(), uuid.New(), 1000, "")
	if err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
	}
}

func TestListPayments(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID}
	repo.suppliers[supplier.ID] = supplier
	repo.payments[supplier.ID] = []Payment{
		{ID: uuid.New(), Amount: 1000, Note: "First"},
		{ID: uuid.New(), Amount: 2000, Note: "Second"},
	}

	payments, err := svc.ListPayments(supplier.ID, businessID)
	if err != nil {
		t.Fatalf("ListPayments failed: %v", err)
	}
	if len(payments) != 2 {
		t.Fatalf("len = %d, want 2", len(payments))
	}
	if payments[0].Amount != 1000 || payments[1].Amount != 2000 {
		t.Fatalf("expected insertion order [1000, 2000], got [%v, %v]", payments[0].Amount, payments[1].Amount)
	}
}
