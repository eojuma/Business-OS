package deliveries

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeRepo struct {
	deliveries map[uuid.UUID]*Delivery
	history    []DeliveryStatusHistory
	createErr  error
	updateErr  error
	getErr     error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{deliveries: map[uuid.UUID]*Delivery{}}
}

func (f *fakeRepo) Create(d *Delivery) error {
	if f.createErr != nil {
		return f.createErr
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	clone := *d
	items := make([]DeliveryItem, len(d.Items))
	copy(items, d.Items)
	clone.Items = items
	f.deliveries[clone.ID] = &clone
	d.ID = clone.ID
	return nil
}

func (f *fakeRepo) CreateDeliveryWithHistory(d *Delivery, h *DeliveryStatusHistory) error {
	if f.createErr != nil {
		return f.createErr
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	clone := *d
	items := make([]DeliveryItem, len(d.Items))
	copy(items, d.Items)
	clone.Items = items
	f.deliveries[clone.ID] = &clone
	d.ID = clone.ID
	if h != nil {
		h.DeliveryID = d.ID
		f.history = append(f.history, *h)
	}
	return nil
}

func (f *fakeRepo) TransitionDelivery(d *Delivery, from DeliveryStatus, h *DeliveryStatusHistory) (bool, error) {
	if f.updateErr != nil {
		return false, f.updateErr
	}
	existing, ok := f.deliveries[d.ID]
	if !ok || existing.Status != from {
		return false, nil
	}
	clone := *d
	items := make([]DeliveryItem, len(d.Items))
	copy(items, d.Items)
	clone.Items = items
	f.deliveries[clone.ID] = &clone
	if h != nil {
		f.history = append(f.history, *h)
	}
	return true, nil
}

func (f *fakeRepo) SaleExists(businessID, id uuid.UUID) (bool, error)     { return true, nil }
func (f *fakeRepo) PurchaseExists(businessID, id uuid.UUID) (bool, error) { return true, nil }
func (f *fakeRepo) ProductExists(businessID, id uuid.UUID) (bool, error)  { return true, nil }

func (f *fakeRepo) GetByID(id, businessID uuid.UUID) (*Delivery, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	d, ok := f.deliveries[id]
	if !ok || d.BusinessID != businessID {
		return nil, ErrDeliveryNotFound
	}
	clone := *d
	items := make([]DeliveryItem, len(d.Items))
	copy(items, d.Items)
	clone.Items = items
	return &clone, nil
}

func (f *fakeRepo) List(businessID uuid.UUID, sourceType string, sourceID *uuid.UUID, status string, limit, offset int) ([]Delivery, int64, error) {
	var out []Delivery
	for _, d := range f.deliveries {
		if d.BusinessID != businessID {
			continue
		}
		if sourceType != "" && d.SourceType != sourceType {
			continue
		}
		if sourceID != nil && d.SourceID != *sourceID {
			continue
		}
		if status != "" && string(d.Status) != status {
			continue
		}
		out = append(out, *d)
	}
	return out, int64(len(out)), nil
}

func (f *fakeRepo) Update(d *Delivery) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	clone := *d
	items := make([]DeliveryItem, len(d.Items))
	copy(items, d.Items)
	clone.Items = items
	f.deliveries[clone.ID] = &clone
	return nil
}

func (f *fakeRepo) Delete(id, businessID uuid.UUID) error {
	d, ok := f.deliveries[id]
	if !ok || d.BusinessID != businessID {
		return ErrDeliveryNotFound
	}
	delete(f.deliveries, id)
	return nil
}

func (f *fakeRepo) CreateHistory(h *DeliveryStatusHistory) error {
	f.history = append(f.history, *h)
	return nil
}

func (f *fakeRepo) ListHistory(deliveryID uuid.UUID) ([]DeliveryStatusHistory, error) {
	var out []DeliveryStatusHistory
	for _, h := range f.history {
		if h.DeliveryID == deliveryID {
			out = append(out, h)
		}
	}
	return out, nil
}

func TestTransitionAllowed(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	bid := uuid.New()
	actor := uuid.New()
	d, err := svc.Create(bid, actor, CreateDeliveryInput{
		SourceType: "sale",
		SourceID:   uuid.New().String(),
		Items: []struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{{ProductID: uuid.New().String(), Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{string(StatusDispatched), string(StatusInTransit), string(StatusDelivered)} {
		got, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: st})
		if err != nil {
			t.Fatalf("transition to %s: %v", st, err)
		}
		if string(got.Status) != st {
			t.Fatalf("status = %s, want %s", got.Status, st)
		}
		d = got
	}
	if len(repo.history) != 4 { // created + 3 transitions
		t.Fatalf("history len = %d, want 4", len(repo.history))
	}
}

func TestTransitionInvalid(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	bid := uuid.New()
	actor := uuid.New()
	d, _ := svc.Create(bid, actor, CreateDeliveryInput{
		SourceType: "sale",
		SourceID:   uuid.New().String(),
		Items: []struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{{ProductID: uuid.New().String(), Quantity: 1}},
	})
	// pending -> delivered is not allowed
	if _, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusDelivered)}); err == nil {
		t.Fatal("expected error for invalid transition")
	}
	// unknown status
	if _, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: "shipped"}); err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestFailedDeliveryPath(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	bid := uuid.New()
	actor := uuid.New()
	d, _ := svc.Create(bid, actor, CreateDeliveryInput{
		SourceType: "purchase",
		SourceID:   uuid.New().String(),
		Items: []struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{{ProductID: uuid.New().String(), Quantity: 3}},
	})
	if _, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusDispatched)}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusFailed), Note: "customer refused"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("status = %s", got.Status)
	}
	if got.FailureReason != "customer refused" {
		t.Fatalf("reason = %s", got.FailureReason)
	}
	if got.FailedAt == nil {
		t.Fatal("failed_at not set")
	}
	// retry: failed -> dispatched
	if _, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusDispatched)}); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestDeliveredTerminal(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	bid := uuid.New()
	actor := uuid.New()
	d, _ := svc.Create(bid, actor, CreateDeliveryInput{
		SourceType: "sale",
		SourceID:   uuid.New().String(),
		Items: []struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{{ProductID: uuid.New().String(), Quantity: 1}},
	})
	_, _ = svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusDispatched)})
	_, _ = svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusDelivered)})
	// delivered is terminal: cannot transition further (except? no)
	if _, err := svc.Transition(bid, actor, d.ID, TransitionInput{Status: string(StatusDispatched)}); err == nil {
		t.Fatal("expected error transitioning from delivered")
	}
}

func TestHistoryAndNote(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	bid := uuid.New()
	actor := uuid.New()
	d, _ := svc.Create(bid, actor, CreateDeliveryInput{
		SourceType: "sale",
		SourceID:   uuid.New().String(),
		Items: []struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{{ProductID: uuid.New().String(), Quantity: 1}},
	})
	hist, err := svc.History(bid, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history = %d", len(hist))
	}
	note, err := svc.DeliveryNote(bid, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if note["delivery_id"] != d.ID {
		t.Fatalf("note delivery_id mismatch")
	}
}

func TestCreateInvalidSourceType(t *testing.T) {
	svc := NewService(newFakeRepo(), nil)
	_, err := svc.Create(uuid.New(), uuid.New(), CreateDeliveryInput{
		SourceType: "foo",
		SourceID:   uuid.New().String(),
		Items: []struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{{ProductID: uuid.New().String(), Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

var _ = gorm.ErrRecordNotFound
