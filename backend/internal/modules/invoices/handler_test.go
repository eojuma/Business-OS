package invoices

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeService struct {
	invoices    []Invoice
	total       int64
	listFilter  ListFilter
	listErr     error
	getInvoice  *Invoice
	getErr      error
	recorded    *Invoice
	recordErr   error
	paymentSeen RecordPaymentInput
	recordCalls int
}

func (f *fakeService) List(businessID uuid.UUID, filter ListFilter) ([]Invoice, int64, error) {
	f.listFilter = filter
	return f.invoices, f.total, f.listErr
}

func (f *fakeService) Get(id, businessID uuid.UUID) (*Invoice, error) {
	return f.getInvoice, f.getErr
}

func (f *fakeService) RecordPayment(id, businessID uuid.UUID, input RecordPaymentInput) (*Invoice, error) {
	f.recordCalls++
	f.paymentSeen = input
	return f.recorded, f.recordErr
}

func invoiceTestRouter(service Service) *gin.Engine {
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextBusinessIDKey, uuid.NewString())
		c.Next()
	})
	router.GET("/invoices", handler.List)
	router.GET("/invoices/:id", handler.Get)
	router.POST("/invoices/:id/payments", handler.RecordPayment)
	return router
}

func TestListParsesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{total: 1, invoices: []Invoice{{ID: uuid.New()}}}
	customerID := uuid.NewString()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/invoices?status=overdue&page=2&limit=5&customer_id="+customerID+"&date_from=2026-01-01&date_to=2026-01-31",
		nil,
	)
	recorder := httptest.NewRecorder()
	invoiceTestRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !service.listFilter.Overdue || service.listFilter.Status != "" {
		t.Fatalf("overdue filter = %+v", service.listFilter)
	}
	if service.listFilter.Page != 2 || service.listFilter.Limit != 5 {
		t.Fatalf("pagination = page %d limit %d", service.listFilter.Page, service.listFilter.Limit)
	}
	if service.listFilter.CustomerID == nil || service.listFilter.CustomerID.String() != customerID {
		t.Fatalf("customer filter = %v", service.listFilter.CustomerID)
	}
	if service.listFilter.DateFrom == nil || service.listFilter.DateTo == nil {
		t.Fatal("expected date filters")
	}
}

func TestListRejectsInvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/invoices?status=void", nil)
	recorder := httptest.NewRecorder()
	invoiceTestRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRecordPaymentMapsOverpayment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{recordErr: ErrPaymentExceedsBalance}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/invoices/"+uuid.NewString()+"/payments", strings.NewReader(`{"amount":500}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	invoiceTestRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if service.paymentSeen.Amount != 500 {
		t.Fatalf("payment amount = %d, want 500", service.paymentSeen.Amount)
	}
}

func TestHandlerGetMapsMissingInvoice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{getErr: ErrInvoiceNotFound}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/invoices/"+uuid.NewString(), nil)
	recorder := httptest.NewRecorder()
	invoiceTestRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
