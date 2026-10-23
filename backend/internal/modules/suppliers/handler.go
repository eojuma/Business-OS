package suppliers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ service Service }

func NewHandler(service Service) *Handler { return &Handler{service: service} }

type createRequest struct {
	Name         string `json:"name" binding:"required"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Address      string `json:"address"`
	PaymentTerms string `json:"payment_terms"`
}
type updateRequest struct {
	Name         *string `json:"name"`
	Phone        *string `json:"phone"`
	Email        *string `json:"email"`
	Address      *string `json:"address"`
	PaymentTerms *string `json:"payment_terms"`
}
type paymentRequest struct {
	Amount int64  `json:"amount" binding:"required"`
	Note   string `json:"note"`
}

func businessAndID(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid supplier id")
		return uuid.Nil, uuid.Nil, false
	}
	return businessID, id, true
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
	supplier, err := h.service.Create(CreateInput{BusinessID: businessID, Name: req.Name, Phone: req.Phone, Email: req.Email, Address: req.Address, PaymentTerms: req.PaymentTerms})
	if errors.Is(err, ErrInvalidPaymentTerms) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create supplier")
		return
	}
	response.Success(c, http.StatusCreated, supplier)
}

func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	includeArchived := false
	if raw := c.Query("include_archived"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid include_archived")
			return
		}
		includeArchived = parsed
	}
	items, err := h.service.List(businessID, includeArchived)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list suppliers")
		return
	}
	response.Success(c, http.StatusOK, items)
}

func (h *Handler) Get(c *gin.Context) {
	businessID, id, ok := businessAndID(c)
	if !ok {
		return
	}
	item, err := h.service.Get(id, businessID)
	if errors.Is(err, ErrSupplierNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch supplier")
		return
	}
	response.Success(c, http.StatusOK, item)
}

func (h *Handler) Update(c *gin.Context) {
	businessID, id, ok := businessAndID(c)
	if !ok {
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.service.Update(id, businessID, UpdateInput{
		Name:         req.Name,
		Phone:        req.Phone,
		Email:        req.Email,
		Address:      req.Address,
		PaymentTerms: req.PaymentTerms,
	})
	if errors.Is(err, ErrSupplierNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, ErrInvalidPaymentTerms) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to update supplier")
		return
	}
	response.Success(c, http.StatusOK, item)
}

func (h *Handler) Delete(c *gin.Context) {
	businessID, id, ok := businessAndID(c)
	if !ok {
		return
	}
	err := h.service.Delete(id, businessID)
	if errors.Is(err, ErrSupplierNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, ErrSupplierHasPurchases) {
		response.Error(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to delete supplier")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Restore(c *gin.Context) {
	businessID, id, ok := businessAndID(c)
	if !ok {
		return
	}
	item, err := h.service.Restore(id, businessID)
	if errors.Is(err, ErrSupplierNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to restore supplier")
		return
	}
	response.Success(c, http.StatusOK, item)
}

func (h *Handler) RecordPayment(c *gin.Context) {
	businessID, id, ok := businessAndID(c)
	if !ok {
		return
	}
	var req paymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.service.RecordPayment(id, businessID, req.Amount, req.Note)
	if errors.Is(err, ErrInvalidPayment) || errors.Is(err, ErrPaymentExceedsBalance) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, ErrSupplierNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to record supplier payment")
		return
	}
	response.Success(c, http.StatusOK, item)
}

func (h *Handler) ListPayments(c *gin.Context) {
	businessID, id, ok := businessAndID(c)
	if !ok {
		return
	}
	items, err := h.service.ListPayments(id, businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list supplier payments")
		return
	}
	response.Success(c, http.StatusOK, items)
}
