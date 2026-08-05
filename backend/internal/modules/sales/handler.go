package sales

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

type saleItemRequest struct {
	ProductID string `json:"product_id" binding:"required,uuid"`
	Quantity  int64  `json:"quantity" binding:"required,gt=0"`
}

type createSaleRequest struct {
	CustomerID *string           `json:"customer_id"`
	Discount   int64             `json:"discount"`
	Note       string            `json:"note"`
	Items      []saleItemRequest `json:"items" binding:"required,min=1"`
}

func (h *Handler) Create(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var req createSaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var customerID *uuid.UUID
	if req.CustomerID != nil {
		parsed, err := uuid.Parse(*req.CustomerID)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid customer_id")
			return
		}
		customerID = &parsed
	}

	items := make([]SaleItemInput, 0, len(req.Items))
	for _, item := range req.Items {
		productID, err := uuid.Parse(item.ProductID)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid product_id")
			return
		}
		items = append(items, SaleItemInput{
			ProductID: productID,
			Quantity:  item.Quantity,
		})
	}

	sale, err := h.service.CreateSale(CreateSaleInput{
		BusinessID: businessID,
		CustomerID: customerID,
		Discount:   req.Discount,
		Note:       req.Note,
		Items:      items,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrEmptySale):
			response.Error(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrProductNotFound):
			response.Error(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInsufficientStock):
			response.Error(c, http.StatusBadRequest, err.Error())
		default:
			response.Error(c, http.StatusInternalServerError, "failed to record sale")
		}
		return
	}

	response.Success(c, http.StatusCreated, sale)
}

func (h *Handler) Get(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid sale id")
		return
	}

	sale, err := h.service.Get(id, businessID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "sale not found")
		return
	}

	response.Success(c, http.StatusOK, sale)
}

func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	sales, err := h.service.List(businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list sales")
		return
	}

	response.Success(c, http.StatusOK, sales)
}
