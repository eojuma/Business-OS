package purchases

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/businessos/backend/internal/modules/suppliers"
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
	Note      string `json:"note"`
}
type createRequest struct {
	SupplierID string        `json:"supplier_id" binding:"required,uuid"`
	AmountPaid int64         `json:"amount_paid"`
	Note       string        `json:"note"`
	ExpectedAt *time.Time    `json:"expected_at"`
	Items      []itemRequest `json:"items" binding:"required,min=1,dive"`
}
type receiveItemRequest struct {
	ProductID   string `json:"product_id" binding:"required,uuid"`
	ReceivedQty int64  `json:"received_qty" binding:"required"`
}
type receiveRequest struct {
	Items []receiveItemRequest `json:"items" binding:"omitempty,dive"`
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
		items = append(items, ItemInput{ProductID: productID, Quantity: item.Quantity, UnitCost: item.UnitCost, Note: item.Note})
	}
	purchase, err := h.service.Create(CreateInput{BusinessID: businessID, SupplierID: supplierID, AmountPaid: req.AmountPaid, Note: req.Note, ExpectedAt: req.ExpectedAt, Items: items})
	if errors.Is(err, suppliers.ErrSupplierNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
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
	page := 1
	if raw := c.Query("page"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		if l, err := strconv.Atoi(raw); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	status := c.Query("status")
	supplierID := c.Query("supplier_id")
	var supplierIDPtr *uuid.UUID
	if raw := supplierID; raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid supplier_id")
			return
		}
		supplierIDPtr = &parsed
	}
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	poNumber := c.Query("po_number")
	var poNumberPtr *string
	if poNumber != "" {
		poNumberPtr = &poNumber
	}
	items, total, err := h.service.ListPaginated(businessID, page, limit, &status, supplierIDPtr, &dateFrom, &dateTo, poNumberPtr)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list purchases")
		return
	}
	totalPages := (total + int64(limit) - 1) / int64(limit)
	response.Success(c, http.StatusOK, gin.H{
		"data": items,
		"meta": gin.H{
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": totalPages,
		},
	})
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
	var req receiveRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	items := make([]ReceiveItemInput, 0, len(req.Items))
	for _, item := range req.Items {
		productID, _ := uuid.Parse(item.ProductID)
		items = append(items, ReceiveItemInput{ProductID: productID, ReceivedQty: item.ReceivedQty})
	}
	item, err := h.service.Receive(id, businessID, ReceiveInput{Items: items})
	if errors.Is(err, ErrPurchaseNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, ErrAlreadyReceived) {
		response.Error(c, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, ErrInvalidReceiveQuantity) || errors.Is(err, ErrUnknownReceiveItem) ||
		errors.Is(err, ErrDuplicateReceiveItem) || errors.Is(err, ErrNothingToReceive) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to receive purchase")
		return
	}
	response.Success(c, http.StatusOK, item)
}
