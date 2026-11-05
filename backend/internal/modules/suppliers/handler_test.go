package suppliers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func agingTestRouter(repo Repository, businessID uuid.UUID) *gin.Engine {
	router := gin.New()
	if businessID != uuid.Nil {
		router.Use(func(c *gin.Context) { c.Set(middleware.ContextBusinessIDKey, businessID.String()) })
	}
	router.GET("/suppliers/aging", NewHandler(NewService(repo)).GetAging)
	return router
}

func TestAgingHandlerAsOf(t *testing.T) {
	businessID := uuid.New()
	for _, tc := range []struct {
		name  string
		query string
		valid bool
	}{
		{name: "defaults to now", valid: true},
		{name: "offset timestamp", query: "?as_of=" + url.QueryEscape("2026-10-30T00:30:00+03:00"), valid: true},
		{name: "fractional seconds", query: "?as_of=2026-10-30T00:30:00.123456Z", valid: true},
		{name: "date without time", query: "?as_of=2026-10-30"},
		{name: "invalid date", query: "?as_of=2026-02-30T00:00:00Z"},
		{name: "year zero", query: "?as_of=0000-01-01T00:00:00Z"},
		{name: "UTC year underflow", query: "?as_of=" + url.QueryEscape("0001-01-01T00:30:00+01:00")},
		{name: "UTC year overflow", query: "?as_of=" + url.QueryEscape("9999-12-31T23:30:00-01:00")},
		{name: "missing timezone", query: "?as_of=2026-10-30T00:30:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured time.Time
			repo := newTestRepository()
			repo.getAging = func(id uuid.UUID, asOf time.Time) ([]AgingSupplier, error) {
				if id != businessID {
					t.Fatalf("business = %s, want %s", id, businessID)
				}
				captured = asOf
				return []AgingSupplier{{ID: uuid.New(), Name: "Supplier", OutstandingBalance: 500, Bucket: AgingCurrent}}, nil
			}
			router := agingTestRouter(repo, businessID)
			response := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/suppliers/aging"+tc.query, nil)
			before := time.Now()
			router.ServeHTTP(response, request)
			after := time.Now()
			if !tc.valid {
				if response.Code != http.StatusBadRequest || !captured.IsZero() {
					t.Fatalf("invalid as_of: status %d, query time %v", response.Code, captured)
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if raw := request.URL.Query().Get("as_of"); raw != "" {
				want, err := time.Parse(time.RFC3339, raw)
				if err != nil || !captured.Equal(want) {
					t.Fatalf("as_of = %v, want %s", captured, raw)
				}
			} else if captured.Before(before) || captured.After(after) {
				t.Fatalf("default as_of %v is outside request interval", captured)
			}
			var body struct {
				Success bool        `json:"success"`
				Data    AgingReport `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !body.Success || !body.Data.AsOf.Equal(captured) || len(body.Data.Current) != 1 || body.Data.Totals[AgingCurrent] != 500 {
				t.Fatalf("unexpected aging response: %+v", body)
			}
			if !strings.Contains(response.Body.String(), `"due_date":null`) || strings.Contains(response.Body.String(), `"bucket"`) {
				t.Fatalf("unexpected supplier JSON: %s", response.Body.String())
			}
		})
	}
}

func TestAgingHandlerErrors(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		repo := newTestRepository()
		called := false
		repo.getAging = func(uuid.UUID, time.Time) ([]AgingSupplier, error) {
			called = true
			return nil, errors.New("private database error")
		}
		id, wantStatus := uuid.Nil, http.StatusUnauthorized
		if authorized {
			id, wantStatus = uuid.New(), http.StatusInternalServerError
		}
		response := httptest.NewRecorder()
		agingTestRouter(repo, id).ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/suppliers/aging", nil))
		if response.Code != wantStatus || called != authorized || strings.Contains(response.Body.String(), "private database error") {
			t.Fatalf("authorized %v: status %d, query called %v, response %s", authorized, response.Code, called, response.Body.String())
		}
	}
}

func TestAgingRoutePrecedesSupplierID(t *testing.T) {
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(func(c *gin.Context) { c.Set(middleware.ContextBusinessIDKey, uuid.New().String()) })
	RegisterRoutes(group, nil, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/suppliers/aging?as_of=invalid", nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "as_of must be") {
		t.Fatalf("aging path did not reach aging handler: %d %s", response.Code, response.Body.String())
	}
}
