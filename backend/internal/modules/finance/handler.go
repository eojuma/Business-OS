package finance

import (
	"errors"
	"net/http"
	"time"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const dateFormat = "2006-01-02"

type Handler struct{ service Service }

func NewHandler(service Service) *Handler { return &Handler{service: service} }

type createExpenseRequest struct {
	Category    string `json:"category" binding:"required"`
	Description string `json:"description"`
	Amount      int64  `json:"amount" binding:"required"`
	IncurredAt  string `json:"incurred_at"`
}

func dateRange(c *gin.Context) (time.Time, time.Time, bool) {
	to := time.Now()
	from := to.AddDate(0, 0, -30)
	var err error
	if raw := c.Query("from"); raw != "" {
		from, err = time.Parse(dateFormat, raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid from date, expected YYYY-MM-DD")
			return time.Time{}, time.Time{}, false
		}
	}
	if raw := c.Query("to"); raw != "" {
		parsed, err := time.Parse(dateFormat, raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid to date, expected YYYY-MM-DD")
			return time.Time{}, time.Time{}, false
		}
		to = parsed.Add(24*time.Hour - time.Nanosecond)
	}
	return from, to, true
}
func (h *Handler) CreateExpense(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	var req createExpenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	var incurredAt time.Time
	if req.IncurredAt != "" {
		incurredAt, err = time.Parse(dateFormat, req.IncurredAt)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid incurred_at, expected YYYY-MM-DD")
			return
		}
	}
	item, err := h.service.CreateExpense(CreateExpenseInput{BusinessID: businessID, Category: req.Category, Description: req.Description, Amount: req.Amount, IncurredAt: incurredAt})
	if errors.Is(err, ErrInvalidExpense) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create expense")
		return
	}
	response.Success(c, http.StatusCreated, item)
}
func (h *Handler) ListExpenses(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	from, to, ok := dateRange(c)
	if !ok {
		return
	}
	items, err := h.service.ListExpenses(businessID, from, to)
	if errors.Is(err, ErrInvalidDateRange) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list expenses")
		return
	}
	response.Success(c, http.StatusOK, items)
}
func (h *Handler) DeleteExpense(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid expense id")
		return
	}
	err = h.service.DeleteExpense(id, businessID)
	if errors.Is(err, ErrExpenseNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to delete expense")
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *Handler) Summary(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	from, to, ok := dateRange(c)
	if !ok {
		return
	}
	item, err := h.service.Summary(businessID, from, to)
	if errors.Is(err, ErrInvalidDateRange) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate finance summary")
		return
	}
	response.Success(c, http.StatusOK, item)
}
