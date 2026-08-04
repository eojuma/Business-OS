package products

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

type createRequest struct {
	Name     string `json:"name" binding:"required"`
	Category string `json:"category"`
	Unit     string `json:"unit" binding:"required"`
	Price    int64  `json:"price" binding:"required"` // cents
	SKU      string `json:"sku"`
}

func (h *Handler) Create(c *gin.Context) {
	businessID, err := currentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	p, err := h.service.Create(CreateInput{
		BusinessID: businessID,
		Name:       req.Name,
		Category:   req.Category,
		Unit:       req.Unit,
		Price:      req.Price,
		SKU:        req.SKU,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidPrice) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to create product")
		return
	}

	response.Success(c, http.StatusCreated, p)
}

func (h *Handler) List(c *gin.Context) {
	businessID, err := currentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	products, err := h.service.List(businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list products")
		return
	}

	response.Success(c, http.StatusOK, products)
}

func (h *Handler) Get(c *gin.Context) {
	businessID, err := currentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product id")
		return
	}

	p, err := h.service.Get(id, businessID)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to fetch product")
		return
	}

	response.Success(c, http.StatusOK, p)
}

type updateRequest struct {
	Name     *string `json:"name"`
	Category *string `json:"category"`
	Unit     *string `json:"unit"`
	Price    *int64  `json:"price"`
	SKU      *string `json:"sku"`
}

func (h *Handler) Update(c *gin.Context) {
	businessID, err := currentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product id")
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	p, err := h.service.Update(id, businessID, UpdateInput{
		Name:     req.Name,
		Category: req.Category,
		Unit:     req.Unit,
		Price:    req.Price,
		SKU:      req.SKU,
	})
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, ErrInvalidPrice) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to update product")
		return
	}

	response.Success(c, http.StatusOK, p)
}

func (h *Handler) Delete(c *gin.Context) {
	businessID, err := currentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid product id")
		return
	}

	if err := h.service.Delete(id, businessID); err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to delete product")
		return
	}

	response.Success(c, http.StatusOK, gin.H{"deleted": true})
}


func currentBusinessID(c *gin.Context) (uuid.UUID, error) {
	raw, exists := c.Get(middleware.ContextBusinessIDKey)
	if !exists {
		return uuid.Nil, errors.New("missing business context")
	}
	str, ok := raw.(string)
	if !ok {
		return uuid.Nil, errors.New("invalid business context")
	}
	return uuid.Parse(str)
}