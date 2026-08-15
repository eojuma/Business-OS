package notifications

import (
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Handler struct{ service Service }

func NewHandler(service Service) *Handler { return &Handler{service: service} }
func (h *Handler) List(c *gin.Context) {
	id, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := h.service.List(id)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	response.Success(c, http.StatusOK, items)
}
