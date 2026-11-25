package deliveries

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler { return &Handler{service: service} }

type createRequest struct {
	SourceType     string `json:"source_type" binding:"required"`
	SourceID       string `json:"source_id" binding:"required,uuid"`
	TrackingNumber string `json:"tracking_number"`
	Carrier        string `json:"carrier"`
	Notes          string `json:"notes"`
	Items          []struct {
		ProductID string `json:"product_id" binding:"required,uuid"`
		Quantity  int64  `json:"quantity" binding:"required,gt=0"`
	} `json:"items" binding:"required,min=1"`
}

type transitionRequest struct {
	Status string `json:"status" binding:"required"`
	Note   string `json:"note"`
}

func (h *Handler) Create(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	userIDStr, _ := c.Get(middleware.ContextUserIDKey)
	var actorID uuid.UUID
	if s, ok := userIDStr.(string); ok {
		actorID, _ = uuid.Parse(s)
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	input := CreateDeliveryInput{
		SourceType:     req.SourceType,
		SourceID:       req.SourceID,
		TrackingNumber: req.TrackingNumber,
		Carrier:        req.Carrier,
		Notes:          req.Notes,
	}
	for _, it := range req.Items {
		input.Items = append(input.Items, struct {
			ProductID string `json:"product_id" binding:"required,uuid"`
			Quantity  int64  `json:"quantity" binding:"required,gt=0"`
		}{ProductID: it.ProductID, Quantity: it.Quantity})
	}
	d, err := h.service.Create(businessID, actorID, input)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, http.StatusCreated, d)
}

func (h *Handler) Get(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := h.service.Get(businessID, id)
	if err != nil {
		if errors.Is(err, ErrDeliveryNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, http.StatusOK, d)
}

func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	sourceType := c.Query("source_type")
	status := c.Query("status")
	var sourceID *uuid.UUID
	if v := c.Query("source_id"); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid source_id")
			return
		}
		sourceID = &parsed
	}
	limit := 50
	offset := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	items, total, err := h.service.List(businessID, sourceType, sourceID, status, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, http.StatusOK, gin.H{"items": items, "total": total})
}

func (h *Handler) Transition(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	userIDStr, _ := c.Get(middleware.ContextUserIDKey)
	var actorID uuid.UUID
	if s, ok := userIDStr.(string); ok {
		actorID, _ = uuid.Parse(s)
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req transitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	d, err := h.service.Transition(businessID, actorID, id, TransitionInput{Status: req.Status, Note: req.Note})
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, http.StatusOK, d)
}

func (h *Handler) Delete(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.service.Delete(businessID, id); err != nil {
		if errors.Is(err, ErrDeliveryNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, http.StatusOK, gin.H{"deleted": true})
}

func (h *Handler) History(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := h.service.History(businessID, id)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, http.StatusOK, items)
}

func (h *Handler) Note(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	note, err := h.service.DeliveryNote(businessID, id)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, http.StatusOK, note)
}
