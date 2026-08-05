package inventory

import (
	"errors"
	"net/http"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

type recordMovementRequest struct {
	ProductID string       `json:"product_id" binding:"required,uuid"`
	Type      MovementType `json:"type" binding:"required"`
	Quantity  int64        `json:"quantity" binding:"required,gt=0"`
	Note      string       `json:"note"`
}

func (h *Handler) RecordMovement(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var req recordMovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product_id")
		return
	}

	level, err := h.service.RecordMovement(RecordMovementInput{
		BusinessID: businessID,
		ProductID:  productID,
		Type:       req.Type,
		Quantity:   req.Quantity,
		Note:       req.Note,
	})
	if err != nil {
		if errors.Is(err, ErrInsufficientStock) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to record stock movement")
		return
	}

	response.Success(c, http.StatusCreated, level)
}

func (h *Handler) GetStockLevel(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	productID, err := uuid.Parse(c.Param("productId"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product id")
		return
	}

	level, err := h.service.GetStockLevel(productID, businessID)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to fetch stock level")
		return
	}

	response.Success(c, http.StatusOK, level)
}

func (h *Handler) ListLowStock(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	levels, err := h.service.ListLowStock(businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list low stock products")
		return
	}

	response.Success(c, http.StatusOK, levels)
}

func (h *Handler) ListMovements(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	productID, err := uuid.Parse(c.Param("productId"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product id")
		return
	}

	movements, err := h.service.ListMovements(productID, businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list stock movements")
		return
	}

	response.Success(c, http.StatusOK, movements)
}