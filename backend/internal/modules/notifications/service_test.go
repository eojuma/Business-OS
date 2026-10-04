package notifications

import (
	"testing"

	"github.com/google/uuid"
)

type testRepository struct {
	notifications map[uuid.UUID]*Notification
}

func newTestRepository() *testRepository {
	return &testRepository{
		notifications: make(map[uuid.UUID]*Notification),
	}
}

func (r *testRepository) Create(n *Notification) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	r.notifications[n.ID] = n
	return nil
}

func (r *testRepository) List(businessID uuid.UUID, unreadOnly bool) ([]Notification, error) {
	var result []Notification
	for _, n := range r.notifications {
		if n.BusinessID == businessID {
			if !unreadOnly || !n.Read {
				result = append(result, *n)
			}
		}
	}
	return result, nil
}

func (r *testRepository) UnreadCount(businessID uuid.UUID) (int64, error) {
	var count int64
	for _, n := range r.notifications {
		if n.BusinessID == businessID && !n.Read {
			count++
		}
	}
	return count, nil
}

func (r *testRepository) MarkRead(id, businessID uuid.UUID) error {
	n, ok := r.notifications[id]
	if !ok || n.BusinessID != businessID {
		return ErrNotFound
	}
	n.Read = true
	return nil
}

func (r *testRepository) MarkAllRead(businessID uuid.UUID) error {
	for _, n := range r.notifications {
		if n.BusinessID == businessID {
			n.Read = true
		}
	}
	return nil
}

func (r *testRepository) MarkReadByEntity(businessID uuid.UUID, notificationType string, entityID uuid.UUID) error {
	for _, n := range r.notifications {
		if n.BusinessID == businessID && n.Type == notificationType && n.EntityID != nil && *n.EntityID == entityID && !n.Read {
			n.Read = true
		}
	}
	return nil
}

func (r *testRepository) ExistsUnread(businessID uuid.UUID, notificationType string, entityID uuid.UUID) (bool, error) {
	for _, n := range r.notifications {
		if n.BusinessID == businessID && n.Type == notificationType && n.EntityID != nil && *n.EntityID == entityID && !n.Read {
			return true, nil
		}
	}
	return false, nil
}

func (r *testRepository) FindByID(id, businessID uuid.UUID) (*Notification, error) {
	n, ok := r.notifications[id]
	if !ok || n.BusinessID != businessID {
		return nil, ErrNotFound
	}
	return n, nil
}

func (r *testRepository) Delete(id, businessID uuid.UUID) error {
	n, ok := r.notifications[id]
	if !ok || n.BusinessID != businessID {
		return ErrNotFound
	}
	delete(r.notifications, id)
	return nil
}

func (r *testRepository) Update(n *Notification) error {
	if _, ok := r.notifications[n.ID]; !ok {
		return ErrNotFound
	}
	r.notifications[n.ID] = n
	return nil
}

func newTestService() (Service, *testRepository) {
	repo := newTestRepository()
	return NewService(repo), repo
}

// TestServiceList verifies listing notifications returns only those belonging to the business.
func TestServiceList(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "A", Read: false}
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "B", Read: true}
	repo.notifications[uuid.New()] = &Notification{BusinessID: uuid.New(), Title: "C", Read: false}

	list, err := svc.List(businessID, false)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

// TestServiceListUnreadOnly verifies listing with unreadOnly filter.
func TestServiceListUnreadOnly(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "Unread", Read: false}
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "Read", Read: true}
	repo.notifications[uuid.New()] = &Notification{BusinessID: uuid.New(), Title: "Other Business", Read: false}

	list, err := svc.List(businessID, true)
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

// TestServiceUnreadCount verifies counting unread notifications.
func TestServiceUnreadCount(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "A", Read: false}
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "B", Read: true}
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "C", Read: false}
	repo.notifications[uuid.New()] = &Notification{BusinessID: uuid.New(), Title: "Other", Read: false}

	count, err := svc.UnreadCount(businessID)
	if err != nil {
		t.Fatalf("UnreadCount failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

// TestServiceMarkRead verifies marking a notification as read.
func TestServiceMarkRead(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	id := uuid.New()
	repo.notifications[id] = &Notification{ID: id, BusinessID: businessID, Title: "Test", Read: false}

	err := svc.MarkRead(id, businessID)
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}
	if repo.notifications[id].Read != true {
		t.Fatal("notification should be marked as read")
	}
}

// TestServiceMarkReadNotFound verifies MarkRead returns ErrNotFound for non-existent notifications.
func TestServiceMarkReadNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	err := svc.MarkRead(uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestServiceMarkReadWrongBusiness verifies MarkRead returns ErrNotFound for wrong business.
func TestServiceMarkReadWrongBusiness(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	id := uuid.New()
	repo.notifications[id] = &Notification{ID: id, BusinessID: businessID, Title: "Test"}

	err := svc.MarkRead(id, uuid.New())
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for wrong business, got %v", err)
	}
}

// TestServiceMarkAllRead verifies marking all notifications as read.
func TestServiceMarkAllRead(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "A", Read: false}
	repo.notifications[uuid.New()] = &Notification{BusinessID: businessID, Title: "B", Read: false}

	err := svc.MarkAllRead(businessID)
	if err != nil {
		t.Fatalf("MarkAllRead failed: %v", err)
	}
	for _, n := range repo.notifications {
		if n.BusinessID == businessID && !n.Read {
			t.Fatal("all notifications should be marked as read")
		}
	}
}