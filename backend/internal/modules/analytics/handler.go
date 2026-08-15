package analytics

import (
	"errors"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

const dateFormat = "2006-01-02"

type Handler struct{ service Service }

func NewHandler(service Service) *Handler { return &Handler{service: service} }
func rangeFromQuery(c *gin.Context) (time.Time, time.Time, bool) {
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
		to, err = time.Parse(dateFormat, raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid to date, expected YYYY-MM-DD")
			return time.Time{}, time.Time{}, false
		}
		to = to.Add(24*time.Hour - time.Nanosecond)
	}
	return from, to, true
}
func (h *Handler) Overview(c *gin.Context) {
	id, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	from, to, ok := rangeFromQuery(c)
	if !ok {
		return
	}
	item, err := h.service.Overview(id, from, to)
	if errors.Is(err, ErrInvalidRange) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate analytics overview")
		return
	}
	response.Success(c, http.StatusOK, item)
}
func (h *Handler) TopProducts(c *gin.Context) {
	id, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	from, to, ok := rangeFromQuery(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	items, err := h.service.TopProducts(id, from, to, limit)
	if errors.Is(err, ErrInvalidRange) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate product analytics")
		return
	}
	response.Success(c, http.StatusOK, items)
}
func (h *Handler) SlowMoving(c *gin.Context) {
	id, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "60"))
	items, err := h.service.SlowMoving(id, days)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate slow-moving product analytics")
		return
	}
	response.Success(c, http.StatusOK, items)
}
