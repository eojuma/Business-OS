package suppliers

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type testRepository struct {
	suppliers map[uuid.UUID]*Supplier
	payments  map[uuid.UUID][]Payment
	getAging  func(uuid.UUID, time.Time) ([]AgingSupplier, error)
}

func (r *testRepository) GetAging(businessID uuid.UUID, asOf time.Time) ([]AgingSupplier, error) {
	if r.getAging != nil {
		return r.getAging(businessID, asOf)
	}
	return nil, nil
}

func TestGetAgingGroupsSuppliersAndTotals(t *testing.T) {
	businessID := uuid.New()
	asOf := time.Date(2026, time.October, 30, 3, 0, 0, 0, time.FixedZone("EAT", 3*60*60))
	dueDate := asOf.AddDate(0, 0, -1)
	buckets := []AgingBucket{AgingCurrent, AgingDays1To30, AgingDays31To60, AgingDays61To90, AgingDays90Plus}
	rows := make([]AgingSupplier, 0, len(buckets)+1)
	for _, bucket := range buckets {
		rows = append(rows, AgingSupplier{ID: uuid.New(), Name: string(bucket), OutstandingBalance: 100, Bucket: bucket})
	}
	rows = append(rows, AgingSupplier{ID: uuid.New(), Name: "Second overdue supplier", OutstandingBalance: 250, DueDate: &dueDate, DaysOverdue: 1, Bucket: AgingDays1To30})
	repo := newTestRepository()
	repo.getAging = func(gotID uuid.UUID, gotAsOf time.Time) ([]AgingSupplier, error) {
		if gotID != businessID || !gotAsOf.Equal(asOf) || gotAsOf.Location() != time.UTC {
			t.Fatalf("aging query = %s, %v; want %s, %v in UTC", gotID, gotAsOf, businessID, asOf)
		}
		return rows, nil
	}
	report, err := NewService(repo).GetAging(businessID, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if !report.AsOf.Equal(asOf) {
		t.Fatalf("as_of = %v, want %v", report.AsOf, asOf)
	}
	groups := map[AgingBucket][]AgingSupplier{
		AgingCurrent: report.Current, AgingDays1To30: report.Days1To30,
		AgingDays31To60: report.Days31To60, AgingDays61To90: report.Days61To90, AgingDays90Plus: report.Days90Plus,
	}
	for _, bucket := range buckets {
		wantCount, wantTotal := 1, int64(100)
		if bucket == AgingDays1To30 {
			wantCount, wantTotal = 2, 350
		}
		if len(groups[bucket]) != wantCount || report.Totals[bucket] != wantTotal {
			t.Fatalf("bucket %s = %d suppliers, %d total; want %d, %d", bucket, len(groups[bucket]), report.Totals[bucket], wantCount, wantTotal)
		}
	}
	got := report.Days1To30[1]
	if got.ID != rows[5].ID || got.Name != rows[5].Name || got.DaysOverdue != 1 || got.DueDate == nil || !got.DueDate.Equal(dueDate) {
		t.Fatalf("supplier details were lost: %+v", got)
	}
}

func TestGetAgingEmptyReportJSON(t *testing.T) {
	report, err := NewService(newTestRepository()).GetAging(uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	for _, bucket := range []AgingBucket{AgingCurrent, AgingDays1To30, AgingDays31To60, AgingDays61To90, AgingDays90Plus} {
		if string(data[string(bucket)]) != "[]" {
			t.Fatalf("empty bucket %s = %s, want []", bucket, data[string(bucket)])
		}
		if total, ok := report.Totals[bucket]; !ok || total != 0 {
			t.Fatalf("empty total %s = %d, present %v", bucket, total, ok)
		}
	}
}

func TestGetAgingErrors(t *testing.T) {
	queryError := errors.New("query failed")
	for _, tc := range []struct {
		name string
		rows []AgingSupplier
		err  error
		want error
	}{
		{name: "repository failure", err: queryError, want: queryError},
		{name: "total overflow", rows: []AgingSupplier{
			{Bucket: AgingCurrent, OutstandingBalance: math.MaxInt64},
			{Bucket: AgingCurrent, OutstandingBalance: 1},
		}, want: ErrAgingTotalOverflow},
		{name: "unknown bucket", rows: []AgingSupplier{{Bucket: "invalid", OutstandingBalance: 100}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepository()
			repo.getAging = func(uuid.UUID, time.Time) ([]AgingSupplier, error) { return tc.rows, tc.err }
			report, err := NewService(repo).GetAging(uuid.New(), time.Now())
			if report != nil || err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("GetAging = %+v, %v; want nil report and %v", report, err, tc.want)
			}
		})
	}
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

func (r *testRepository) ListPaginated(businessID uuid.UUID, page, limit int, search string, includeArchived bool) ([]Supplier, int64, error) {
	term := strings.ToLower(strings.TrimSpace(search))
	var result []Supplier
	for _, s := range r.suppliers {
		if s.BusinessID != businessID {
			continue
		}
		if s.DeletedAt.Valid && !includeArchived {
			continue
		}
		if term != "" && !strings.Contains(strings.ToLower(s.Name+" "+s.Phone+" "+s.Email), term) {
			continue
		}
		result = append(result, *s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	total := int64(len(result))
	start := (page - 1) * limit
	if start > len(result) {
		start = len(result)
	}
	end := start + limit
	if end > len(result) {
		end = len(result)
	}
	return result[start:end], total, nil
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

func TestListSuppliersPaginated(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "Alpha", Phone: "0700", Email: "alpha@example.com"}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "Bravo", Phone: "0711", Email: "bravo@example.com"}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "Charlie", Phone: "0722", Email: "charlie@example.com"}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: businessID, Name: "Archived", DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}}
	repo.suppliers[uuid.New()] = &Supplier{BusinessID: uuid.New(), Name: "Other"}

	items, total, err := svc.ListPaginated(businessID, 1, 2, "", false)
	if err != nil {
		t.Fatalf("ListPaginated failed: %v", err)
	}
	if total != 3 || len(items) != 2 || items[0].Name != "Alpha" || items[1].Name != "Bravo" {
		t.Fatalf("page 1 total = %d, len = %d", total, len(items))
	}

	items, total, err = svc.ListPaginated(businessID, 2, 2, "", false)
	if err != nil {
		t.Fatalf("ListPaginated page 2 failed: %v", err)
	}
	if total != 3 || len(items) != 1 || items[0].Name != "Charlie" {
		t.Fatalf("page 2 total = %d, len = %d", total, len(items))
	}

	items, total, err = svc.ListPaginated(businessID, 1, 10, "  bravo  ", false)
	if err != nil {
		t.Fatalf("ListPaginated search failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Bravo" {
		t.Fatalf("search total = %d, len = %d", total, len(items))
	}

	_, total, err = svc.ListPaginated(businessID, 1, 10, "", true)
	if err != nil {
		t.Fatalf("ListPaginated including archived failed: %v", err)
	}
	if total != 4 {
		t.Fatalf("total including archived = %d, want 4", total)
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
