package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeService struct {
	entries []Entry
	total   int64
	filter  ListFilter
	err     error
}

// List records the filter and returns the configured fake response.
func (f *fakeService) List(businessID uuid.UUID, filter ListFilter) ([]Entry, int64, error) {
	f.filter = filter
	return f.entries, f.total, f.err
}

// auditViewerRouter mounts the audit handler with a business context.
func auditViewerRouter(service Service) *gin.Engine {
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextBusinessIDKey, uuid.NewString())
		c.Next()
	})
	router.GET("/audit", handler.List)
	return router
}

// TestAuditListParsesFilters documents its scenario and asserts the expected audit behaviour.
func TestAuditListParsesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{total: 1, entries: []Entry{{ID: uuid.New()}}}
	actorID := uuid.NewString()
	entityID := uuid.NewString()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/audit?actor_id="+actorID+"&action=payment&entity_type=invoice&entity_id="+entityID+
			"&date_from=2026-01-01&date_to=2026-01-31&page=2&limit=5",
		nil,
	)
	recorder := httptest.NewRecorder()
	auditViewerRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if service.filter.ActorID == nil || service.filter.ActorID.String() != actorID {
		t.Fatalf("actor filter = %v", service.filter.ActorID)
	}
	if service.filter.EntityID == nil || service.filter.EntityID.String() != entityID {
		t.Fatalf("entity filter = %v", service.filter.EntityID)
	}
	if service.filter.Action != ActionPayment || service.filter.EntityType != "invoice" {
		t.Fatalf("text filters = %+v", service.filter)
	}
	if service.filter.Page != 2 || service.filter.Limit != 5 {
		t.Fatalf("pagination = %+v", service.filter)
	}
	if service.filter.DateFrom == nil || service.filter.DateTo == nil {
		t.Fatal("expected date filters")
	}
}

// TestAuditListRejectsOverflowingPagination documents its scenario and asserts the expected audit behaviour.
func TestAuditListRejectsOverflowingPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{err: ErrInvalidPagination}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/audit?page=461168601842738792&limit=20", nil)
	recorder := httptest.NewRecorder()
	auditViewerRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

// TestAuditListRejectsInvalidDateRange documents its scenario and asserts the expected audit behaviour.
func TestAuditListRejectsInvalidDateRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{err: ErrInvalidDateRange}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/audit?date_from=2026-02-01&date_to=2026-01-01", nil)
	recorder := httptest.NewRecorder()
	auditViewerRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

// TestAuditListRejectsInvalidActorID documents its scenario and asserts the expected audit behaviour.
func TestAuditListRejectsInvalidActorID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/audit?actor_id=not-a-uuid", nil)
	recorder := httptest.NewRecorder()
	auditViewerRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
