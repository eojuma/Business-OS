package audit

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

// Handler serves the read-only audit viewer API.
type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler { return &Handler{service: service} }

// List returns filtered, paginated audit entries for the current business.
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

	filter := ListFilter{
		Page:       page,
		Limit:      limit,
		Action:     c.Query("action"),
		EntityType: c.Query("entity_type"),
	}
	if raw := c.Query("actor_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid actor_id")
			return
		}
		filter.ActorID = &id
	}
	if raw := c.Query("entity_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid entity_id")
			return
		}
		filter.EntityID = &id
	}
	if filter.DateFrom, err = dateQuery(c, "date_from", false); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid date_from")
		return
	}
	if filter.DateTo, err = dateQuery(c, "date_to", true); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid date_to")
		return
	}

	entries, total, err := h.service.List(businessID, filter)
	if errors.Is(err, ErrInvalidDateRange) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load audit log")
		return
	}
	filter = normalizeFilter(filter)
	response.Paginated(c, http.StatusOK, entries, filter.Page, filter.Limit, total)
}

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
