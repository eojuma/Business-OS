package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const testUnreadTitle = "Unread"

type mockService struct {
	listFunc    func(businessID uuid.UUID, unreadOnly bool) ([]Notification, error)
	unreadCount func(businessID uuid.UUID) (int64, error)
	markRead    func(id, businessID uuid.UUID) error
	markAllRead func(businessID uuid.UUID) error
}

func (m *mockService) List(businessID uuid.UUID, unreadOnly bool) ([]Notification, error) {
	if m.listFunc != nil {
		return m.listFunc(businessID, unreadOnly)
	}
	return nil, nil
}

func (m *mockService) UnreadCount(businessID uuid.UUID) (int64, error) {
	if m.unreadCount != nil {
		return m.unreadCount(businessID)
	}
	return 0, nil
}

func (m *mockService) MarkRead(id, businessID uuid.UUID) error {
	if m.markRead != nil {
		return m.markRead(id, businessID)
	}
	return nil
}

func (m *mockService) MarkAllRead(businessID uuid.UUID) error {
	if m.markAllRead != nil {
		return m.markAllRead(businessID)
	}
	return nil
}

func setupHandler(m *mockService) (*gin.Engine, *Handler) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(m)

	r.Use(func(c *gin.Context) {
		businessID := uuid.New()
		c.Set(middleware.ContextBusinessIDKey, businessID.String())
		c.Next()
	})

	r.GET("/notifications", h.List)
	r.GET("/notifications/unread-count", h.UnreadCount)
	r.POST("/notifications/:id/read", h.MarkRead)
	r.POST("/notifications/read-all", h.MarkAllRead)
	return r, h
}

// TestHandlerList verifies listing notifications returns 200 with data.
func TestHandlerList(t *testing.T) {
	mock := &mockService{
		listFunc: func(businessID uuid.UUID, unreadOnly bool) ([]Notification, error) {
			return []Notification{{ID: uuid.New(), BusinessID: uuid.New(), Title: "Test", Read: false}}, nil
		},
	}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/notifications", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandlerListUnreadOnly verifies listing with unread filter.
func TestHandlerListUnreadOnly(t *testing.T) {
	mock := &mockService{
		listFunc: func(businessID uuid.UUID, unreadOnly bool) ([]Notification, error) {
			if !unreadOnly {
				t.Fatal("expected unreadOnly=true")
			}
			return []Notification{{ID: uuid.New(), Title: testUnreadTitle}}, nil
		},
	}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/notifications?unread=true", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandlerListUnauthorized verifies 401 when no business ID in context.
func TestHandlerListUnauthorized(t *testing.T) {
	mock := &mockService{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(mock)

	r.GET("/notifications", h.List)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/notifications", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestHandlerUnreadCount verifies getting unread count.
func TestHandlerUnreadCount(t *testing.T) {
	mock := &mockService{
		unreadCount: func(businessID uuid.UUID) (int64, error) {
			return 5, nil
		},
	}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/notifications/unread-count", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandlerMarkRead verifies marking a notification as read.
func TestHandlerMarkRead(t *testing.T) {
	id := uuid.New()
	mock := &mockService{
		markRead: func(reqID, businessID uuid.UUID) error {
			if reqID != id {
				t.Fatalf("expected id %v, got %v", id, reqID)
			}
			return nil
		},
	}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/notifications/"+id.String()+"/read", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandlerMarkReadNotFound verifies 404 for non-existent notification.
func TestHandlerMarkReadNotFound(t *testing.T) {
	mock := &mockService{
		markRead: func(id, businessID uuid.UUID) error {
			return ErrNotFound
		},
	}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/notifications/"+uuid.New().String()+"/read", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestHandlerMarkReadInvalidID verifies 400 for invalid UUID.
func TestHandlerMarkReadInvalidID(t *testing.T) {
	mock := &mockService{}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/notifications/invalid/read", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestHandlerMarkReadUnauthorized verifies 401 when no business context.
func TestHandlerMarkReadUnauthorized(t *testing.T) {
	mock := &mockService{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(mock)
	r.POST("/notifications/:id/read", h.MarkRead)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/notifications/"+uuid.New().String()+"/read", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestHandlerMarkAllRead verifies marking all notifications as read.
func TestHandlerMarkAllRead(t *testing.T) {
	mock := &mockService{
		markAllRead: func(businessID uuid.UUID) error {
			return nil
		},
	}
	router, _ := setupHandler(mock)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/notifications/read-all", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandlerMarkAllReadUnauthorized verifies 401 for mark all read.
func TestHandlerMarkAllReadUnauthorized(t *testing.T) {
	mock := &mockService{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(mock)
	r.POST("/notifications/read-all", h.MarkAllRead)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/notifications/read-all", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
