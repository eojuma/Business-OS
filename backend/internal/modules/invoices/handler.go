package invoices

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler exposes invoice reads and payment application.
type Handler struct {
	service Service
}

// NewHandler builds the invoice HTTP handler.
func NewHandler(service Service) *Handler { return &Handler{service: service} }

// List returns filtered, paginated invoices for the current business.
func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	page, err := intQuery(c, "page", 1)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid page")
		return
	}
	limit, err := intQuery(c, "limit", 20)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid limit")
		return
	}

	filter := ListFilter{Page: page, Limit: limit, Status: c.Query("status")}
	if raw := c.Query("customer_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid customer_id")
			return
		}
		filter.CustomerID = &id
	}
	if filter.DateFrom, err = dateQuery(c, "date_from", false); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid date_from")
		return
	}
	if filter.DateTo, err = dateQuery(c, "date_to", true); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid date_to")
		return
	}
	filter, err = NormalizeFilter(filter)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	invoices, total, err := h.service.List(businessID, filter)
	if err != nil {
		if errors.Is(err, ErrInvalidStatusFilter) || errors.Is(err, ErrInvalidDateRange) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to list invoices")
		return
	}
	response.Paginated(c, http.StatusOK, invoices, filter.Page, filter.Limit, total)
}

// Get returns one invoice with its customer and payments.
func (h *Handler) Get(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid invoice id")
		return
	}
	invoice, err := h.service.Get(id, businessID)
	if errors.Is(err, ErrInvoiceNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch invoice")
		return
	}
	response.Success(c, http.StatusOK, invoice)
}

type paymentRequest struct {
	Amount int64  `json:"amount" binding:"required,gt=0"`
	Note   string `json:"note"`
}

// RecordPayment applies a settlement and returns the updated invoice.
func (h *Handler) RecordPayment(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid invoice id")
		return
	}
	var req paymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	invoice, err := h.service.RecordPayment(id, businessID, RecordPaymentInput{Amount: req.Amount, Note: req.Note})
	switch {
	case err == nil:
	case errors.Is(err, ErrInvoiceNotFound):
		response.Error(c, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, ErrInvalidPayment),
		errors.Is(err, ErrPaymentExceedsBalance),
		errors.Is(err, ErrInvoiceAlreadyPaid):
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	default:
		response.Error(c, http.StatusInternalServerError, "failed to record payment")
		return
	}
	response.Success(c, http.StatusCreated, invoice)
}

// intQuery parses a positive integer query parameter with a default.
func intQuery(c *gin.Context, key string, fallback int) (int, error) {
	raw := c.Query(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("invalid integer")
	}
	return value, nil
}

// dateQuery accepts RFC3339 timestamps or date-only values. Date-only upper
// bounds include the whole day.
func dateQuery(c *gin.Context, key string, endOfDay bool) (*time.Time, error) {
	raw := c.Query(key)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	if endOfDay {
		parsed = parsed.Add(24*time.Hour - time.Nanosecond)
	}
	return &parsed, nil
}
