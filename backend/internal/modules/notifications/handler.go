package notifications

import (
	"errors"
	"net/http"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ service Service }

func NewHandler(service Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	unreadOnly := c.Query("unread") == "true"

	items, err := h.service.List(businessID, unreadOnly)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	response.Success(c, http.StatusOK, items)
}

func (h *Handler) UnreadCount(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	count, err := h.service.UnreadCount(businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to count notifications")
		return
	}
	response.Success(c, http.StatusOK, gin.H{"unread": count})
}

func (h *Handler) MarkRead(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid notification id")
		return
	}
	if err := h.service.MarkRead(id, businessID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to update notification")
		return
	}
	response.Success(c, http.StatusOK, gin.H{"id": id, "read": true})
}

func (h *Handler) MarkAllRead(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	if err := h.service.MarkAllRead(businessID); err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to update notifications")
		return
	}
	response.Success(c, http.StatusOK, gin.H{"read": true})
}
