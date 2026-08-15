package purchases

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

type itemRequest struct {
	ProductID string `json:"product_id" binding:"required,uuid"`
	Quantity  int64  `json:"quantity" binding:"required"`
	UnitCost  int64  `json:"unit_cost"`
}
type createRequest struct {
	SupplierID string        `json:"supplier_id" binding:"required,uuid"`
	AmountPaid int64         `json:"amount_paid"`
	Note       string        `json:"note"`
	Items      []itemRequest `json:"items" binding:"required,min=1,dive"`
}

func (h *Handler) Create(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	supplierID, _ := uuid.Parse(req.SupplierID)
	items := make([]ItemInput, 0, len(req.Items))
	for _, item := range req.Items {
		productID, _ := uuid.Parse(item.ProductID)
		items = append(items, ItemInput{ProductID: productID, Quantity: item.Quantity, UnitCost: item.UnitCost})
	}
	purchase, err := h.service.Create(CreateInput{BusinessID: businessID, SupplierID: supplierID, AmountPaid: req.AmountPaid, Note: req.Note, Items: items})
	if errors.Is(err, ErrEmptyPurchase) || errors.Is(err, ErrInvalidPurchaseItem) || errors.Is(err, ErrInvalidAmountPaid) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create purchase")
		return
	}
	response.Success(c, http.StatusCreated, purchase)
}

func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	var supplierID *uuid.UUID
	if raw := c.Query("supplier_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid supplier_id")
			return
		}
		supplierID = &parsed
	}
	items, err := h.service.List(businessID, supplierID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list purchases")
		return
	}
	response.Success(c, http.StatusOK, items)
}

func purchaseIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid purchase id")
		return uuid.Nil, uuid.Nil, false
	}
	return businessID, id, true
}
func (h *Handler) Get(c *gin.Context) {
	businessID, id, ok := purchaseIDs(c)
	if !ok {
		return
	}
	item, err := h.service.Get(id, businessID)
	if errors.Is(err, ErrPurchaseNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch purchase")
		return
	}
	response.Success(c, http.StatusOK, item)
}
func (h *Handler) Receive(c *gin.Context) {
	businessID, id, ok := purchaseIDs(c)
	if !ok {
		return
	}
	item, err := h.service.Receive(id, businessID)
	if errors.Is(err, ErrPurchaseNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, ErrAlreadyReceived) {
		response.Error(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to receive purchase")
		return
	}
	response.Success(c, http.StatusOK, item)
}
