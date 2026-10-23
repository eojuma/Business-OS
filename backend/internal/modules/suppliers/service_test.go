package suppliers

import (
	"testing"
	"time"

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
	if !ok || s.BusinessID != businessID || s.DeletedAt.Valid {
		return nil, gorm.ErrRecordNotFound
	}
	return s, nil
}

func (r *testRepository) List(businessID uuid.UUID, includeArchived bool) ([]Supplier, error) {
	var result []Supplier
	for _, s := range r.suppliers {
		if s.BusinessID != businessID {
			continue
		}
		if s.DeletedAt.Valid && !includeArchived {
			continue
		}
		result = append(result, *s)
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
	if !ok || s.BusinessID != businessID || s.DeletedAt.Valid {
		return gorm.ErrRecordNotFound
	}
	s.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
	return nil
}

func (r *testRepository) Restore(id, businessID uuid.UUID) error {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return ErrSupplierNotFound
	}
	s.DeletedAt = gorm.DeletedAt{}
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

func (r *testRepository) GetAging(businessID uuid.UUID, asOf time.Time) (*AgingReport, error) {
	// Simple implementation for testing - filter suppliers from the map
	var suppliers []*Supplier
	for _, s := range r.suppliers {
		if s.BusinessID == businessID && !s.DeletedAt.Valid && s.OutstandingBalance > 0 {
			suppliers = append(suppliers, s)
		}
	}

	report := &AgingReport{}
	for _, s := range suppliers {
		ds := *s  // dereference pointer
		daysOverdue := 0
		if ds.DueDate != nil && ds.DueDate.Before(asOf) {
			daysOverdue = daysPastDue(*ds.DueDate, asOf)
		}

		entry := AgingSupplier{
			ID:          ds.ID,
			Name:        ds.Name,
			Outstanding: ds.OutstandingBalance,
			DueDate:     ds.DueDate,
			DaysOverdue: daysOverdue,
		}

		switch {
		case daysOverdue <= 0:
			report.Current = append(report.Current, entry)
			report.Totals.Current += s.OutstandingBalance
		case daysOverdue <= 30:
			report.Days1_30 = append(report.Days1_30, entry)
			report.Totals.Days1_30 += s.OutstandingBalance
		case daysOverdue <= 60:
			report.Days31_60 = append(report.Days31_60, entry)
			report.Totals.Days31_60 += s.OutstandingBalance
		case daysOverdue <= 90:
			report.Days61_90 = append(report.Days61_90, entry)
			report.Totals.Days61_90 += s.OutstandingBalance
		default:
			report.Days90Plus = append(report.Days90Plus, entry)
			report.Totals.Days90Plus += s.OutstandingBalance
		}
		report.Totals.Total += s.OutstandingBalance
	}
	return report, nil
}

func (r *testRepository) AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return ErrSupplierNotFound
	}
	s.OutstandingBalance += amount
	return nil
}

func (r *testRepository) SetPaymentDueDateTx(_ *gorm.DB, id, businessID uuid.UUID, receivedAt time.Time) (*time.Time, error) {
	s, ok := r.suppliers[id]
	if !ok || s.BusinessID != businessID {
		return nil, ErrSupplierNotFound
	}
	dueDate, err := DueDateFrom(receivedAt, s.PaymentTerms)
	if err != nil {
		return nil, err
	}
	if s.LastReceivedAt == nil || receivedAt.After(*s.LastReceivedAt) {
		s.DueDate = &dueDate
		s.LastReceivedAt = &receivedAt
	}
	return &dueDate, nil
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
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "Archived", DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: uuid.New(), Name: "C"}

	list, err := svc.List(businessID, false)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}

	all, err := svc.List(businessID, true)
	if err != nil {
		t.Fatalf("List with archived failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3 including archived", len(all))
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
	if !supplier.DeletedAt.Valid {
		t.Fatal("supplier should be archived, not removed")
	}
	if _, err := svc.Get(supplier.ID, businessID); err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound after archive, got %v", err)
	}
}

func TestDeleteSupplierNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	if err := svc.Delete(uuid.New(), uuid.New()); err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
	}
}

func TestRestoreSupplier(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{
		ID:         uuid.New(),
		BusinessID: businessID,
		Name:       "Archived",
		DeletedAt:  gorm.DeletedAt{Time: time.Now(), Valid: true},
	}
	repo.suppliers[supplier.ID] = supplier

	restored, err := svc.Restore(supplier.ID, businessID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if restored.DeletedAt.Valid {
		t.Fatal("supplier should no longer be archived")
	}
	if _, err := svc.Get(supplier.ID, businessID); err != nil {
		t.Fatalf("restored supplier not retrievable: %v", err)
	}
}

func TestRestoreSupplierNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	if _, err := svc.Restore(uuid.New(), uuid.New()); err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
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

func TestDueDateFrom(t *testing.T) {
	received := time.Date(2026, time.January, 10, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		terms    string
		wantDays int
	}{
		{PaymentTermsDueOnReceipt, 0},
		{PaymentTermsNet15, 15},
		{PaymentTermsNet30, 30},
		{PaymentTermsNet45, 45},
		{PaymentTermsNet60, 60},
		{PaymentTermsNet90, 90},
	}
	for _, tt := range tests {
		got, err := DueDateFrom(received, tt.terms)
		if err != nil {
			t.Fatalf("DueDateFrom(%q) failed: %v", tt.terms, err)
		}
		want := received.AddDate(0, 0, tt.wantDays)
		if !got.Equal(want) {
			t.Fatalf("DueDateFrom(%q) = %v, want %v", tt.terms, got, want)
		}
	}
}

func TestDueDateFromInvalidTerms(t *testing.T) {
	_, err := DueDateFrom(time.Now(), "net7")
	if err != ErrInvalidPaymentTerms {
		t.Fatalf("expected ErrInvalidPaymentTerms, got %v", err)
	}
}

func TestCreateSupplierDefaultsPaymentTerms(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	supplier, err := svc.Create(CreateInput{BusinessID: uuid.New(), Name: "Default Terms"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if supplier.PaymentTerms != PaymentTermsDefault {
		t.Fatalf("payment_terms = %q, want %q", supplier.PaymentTerms, PaymentTermsDefault)
	}
}

func TestCreateSupplierWithPaymentTerms(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	supplier, err := svc.Create(CreateInput{BusinessID: uuid.New(), Name: "Net60", PaymentTerms: PaymentTermsNet60})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if supplier.PaymentTerms != PaymentTermsNet60 {
		t.Fatalf("payment_terms = %q, want %q", supplier.PaymentTerms, PaymentTermsNet60)
	}
}

func TestCreateSupplierInvalidPaymentTerms(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.Create(CreateInput{BusinessID: uuid.New(), Name: "Bad Terms", PaymentTerms: "net7"})
	if err != ErrInvalidPaymentTerms {
		t.Fatalf("expected ErrInvalidPaymentTerms, got %v", err)
	}
}

func TestUpdateSupplierPaymentTerms(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Test", PaymentTerms: PaymentTermsNet30}
	repo.suppliers[supplier.ID] = supplier

	terms := PaymentTermsNet90
	updated, err := svc.Update(supplier.ID, businessID, UpdateInput{PaymentTerms: &terms})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.PaymentTerms != PaymentTermsNet90 {
		t.Fatalf("payment_terms = %q, want %q", updated.PaymentTerms, PaymentTermsNet90)
	}
}

func TestUpdateSupplierInvalidPaymentTerms(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Test", PaymentTerms: PaymentTermsNet30}
	repo.suppliers[supplier.ID] = supplier

	terms := "net7"
	_, err := svc.Update(supplier.ID, businessID, UpdateInput{PaymentTerms: &terms})
	if err != ErrInvalidPaymentTerms {
		t.Fatalf("expected ErrInvalidPaymentTerms, got %v", err)
	}
	if supplier.PaymentTerms != PaymentTermsNet30 {
		t.Fatalf("payment_terms should be unchanged, got %q", supplier.PaymentTerms)
	}
}

func TestSetPaymentDueDateTx(t *testing.T) {
	repo := newTestRepository()
	businessID := uuid.New()
	supplier := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Test", PaymentTerms: PaymentTermsNet45}
	repo.suppliers[supplier.ID] = supplier

	received := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)
	dueDate, err := repo.SetPaymentDueDateTx(nil, supplier.ID, businessID, received)
	if err != nil {
		t.Fatalf("SetPaymentDueDateTx failed: %v", err)
	}
	want := received.AddDate(0, 0, 45)
	if dueDate == nil || !dueDate.Equal(want) {
		t.Fatalf("due_date = %v, want %v", dueDate, want)
	}
	if supplier.DueDate == nil || !supplier.DueDate.Equal(want) {
		t.Fatalf("supplier due_date not updated, got %v", supplier.DueDate)
	}
}

func TestSetPaymentDueDateSupplierNotFound(t *testing.T) {
	repo := newTestRepository()
	_, err := repo.SetPaymentDueDateTx(nil, uuid.New(), uuid.New(), time.Now())
	if err != ErrSupplierNotFound {
		t.Fatalf("expected ErrSupplierNotFound, got %v", err)
	}
}


func TestGetAging(t *testing.T) {
	db, businessID := setupTestDB(t)
	repo := NewRepository(db)
	svc := NewService(repo)

	now := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	insert := func(s *Supplier) {
		t.Helper()
		if err := db.Create(s).Error; err != nil {
			t.Fatalf("failed to create supplier: %v", err)
		}
	}

	// Supplier 1: current (not overdue)
	s1 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Current Supplier", OutstandingBalance: 5000, DueDate: func() *time.Time { t := now.AddDate(0, 0, 5); return &t }()}
	insert(s1)

	// Supplier 2: 1-30 days overdue
	s2 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Overdue 1-30", OutstandingBalance: 10000, DueDate: func() *time.Time { t := now.AddDate(0, 0, -10); return &t }()}
	insert(s2)

	// Supplier 3: 31-60 days overdue
	s3 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Overdue 31-60", OutstandingBalance: 15000, DueDate: func() *time.Time { t := now.AddDate(0, 0, -45); return &t }()}
	insert(s3)

	// Supplier 4: 61-90 days overdue
	s4 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Overdue 61-90", OutstandingBalance: 20000, DueDate: func() *time.Time { t := now.AddDate(0, 0, -75); return &t }()}
	insert(s4)

	// Supplier 5: 90+ days overdue
	s5 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Overdue 90+", OutstandingBalance: 25000, DueDate: func() *time.Time { t := now.AddDate(0, 0, -120); return &t }()}
	insert(s5)

	// Supplier 6: no due date (should be current)
	s6 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "No Due Date", OutstandingBalance: 5000, DueDate: nil}
	insert(s6)

	// Supplier with zero balance (should not appear)
	s7 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Zero Balance", OutstandingBalance: 0, DueDate: func() *time.Time { t := now.AddDate(0, 0, -10); return &t }()}
	insert(s7)

	// Supplier 8: archived (deleted_at set) - should not appear
	s8 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Archived", OutstandingBalance: 10000, DueDate: func() *time.Time { t := now.AddDate(0, 0, -10); return &t }(), DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}}
	insert(s8)

	// Supplier 9: overdue by less than 24 hours - still belongs in 1-30
	s9 := &Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Overdue Today", OutstandingBalance: 7000, DueDate: func() *time.Time { t := now.Add(-12 * time.Hour); return &t }()}
	insert(s9)

	asOf := now
	report, err := svc.GetAging(businessID, asOf)
	if err != nil {
		t.Fatalf("GetAging failed: %v", err)
	}

	if len(report.Current) != 2 {
		t.Errorf("Current = %d, want 2 (current + no due date)", len(report.Current))
	}
	if len(report.Days1_30) != 2 {
		t.Errorf("Days1_30 = %d, want 2", len(report.Days1_30))
	}
	for _, entry := range report.Days1_30 {
		if entry.ID == s9.ID && entry.DaysOverdue != 1 {
			t.Errorf("DaysOverdue for sub-day overdue supplier = %d, want 1", entry.DaysOverdue)
		}
	}
	if len(report.Days31_60) != 1 {
		t.Errorf("Days31_60 = %d, want 1", len(report.Days31_60))
	}
	if len(report.Days61_90) != 1 {
		t.Errorf("Days61_90 = %d, want 1", len(report.Days61_90))
	}
	if len(report.Days90Plus) != 1 {
		t.Errorf("Days90Plus = %d, want 1", len(report.Days90Plus))
	}

	// Check totals
	if report.Totals.Current != 10000 {
		t.Errorf("Totals.Current = %d, want 10000", report.Totals.Current)
	}
	if report.Totals.Days1_30 != 17000 {
		t.Errorf("Totals.Days1_30 = %d, want 17000", report.Totals.Days1_30)
	}
	if report.Totals.Days31_60 != 15000 {
		t.Errorf("Totals.Days31_60 = %d, want 15000", report.Totals.Days31_60)
	}
	if report.Totals.Days61_90 != 20000 {
		t.Errorf("Totals.Days61_90 = %d, want 20000", report.Totals.Days61_90)
	}
	if report.Totals.Days90Plus != 25000 {
		t.Errorf("Totals.Days90Plus = %d, want 25000", report.Totals.Days90Plus)
	}
	if report.Totals.Total != 87000 {
		t.Errorf("Totals.Total = %d, want 87000", report.Totals.Total)
	}
}

