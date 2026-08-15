package customers

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

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

type createRequest struct {
	Name        string `json:"name" binding:"required"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	CreditLimit int64  `json:"credit_limit"` // cents
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

	customer, err := h.service.Create(CreateInput{
		BusinessID:  businessID,
		Name:        req.Name,
		Phone:       req.Phone,
		Email:       req.Email,
		CreditLimit: req.CreditLimit,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create customer")
		return
	}

	response.Success(c, http.StatusCreated, customer)
}

func (h *Handler) List(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	customers, err := h.service.List(businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list customers")
		return
	}

	response.Success(c, http.StatusOK, customers)
}

func (h *Handler) Get(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid customer id")
		return
	}

	customer, err := h.service.Get(id, businessID)
	if err != nil {
		if errors.Is(err, ErrCustomerNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to fetch customer")
		return
	}

	response.Success(c, http.StatusOK, customer)
}

type updateRequest struct {
	Name        *string `json:"name"`
	Phone       *string `json:"phone"`
	Email       *string `json:"email"`
	CreditLimit *int64  `json:"credit_limit"`
}

func (h *Handler) Update(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid customer id")
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	customer, err := h.service.Update(id, businessID, UpdateInput{
		Name:        req.Name,
		Phone:       req.Phone,
		Email:       req.Email,
		CreditLimit: req.CreditLimit,
	})
	if err != nil {
		if errors.Is(err, ErrCustomerNotFound) {
			response.Error(c, http.StatusNotFound, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to update customer")
		return
	}

	response.Success(c, http.StatusOK, customer)
}

func (h *Handler) ListAboveBalance(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	thresholdStr := c.DefaultQuery("threshold", "0")
	threshold, err := strconv.ParseInt(thresholdStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid threshold")
		return
	}

	customers, err := h.service.ListAboveBalance(businessID, threshold)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list customers")
		return
	}

	response.Success(c, http.StatusOK, customers)
}

type paymentRequest struct {
	Amount int64  `json:"amount" binding:"required"`
	Note   string `json:"note"`
}

func (h *Handler) RecordPayment(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req paymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	customer, err := h.service.RecordPayment(id, businessID, req.Amount, req.Note)
	if errors.Is(err, ErrInvalidPayment) || errors.Is(err, ErrPaymentExceedsBalance) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, ErrCustomerNotFound) {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to record payment")
		return
	}
	response.Success(c, http.StatusOK, customer)
}

func (h *Handler) ListPayments(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid customer id")
		return
	}
	payments, err := h.service.ListPayments(id, businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list payments")
		return
	}
	response.Success(c, http.StatusOK, payments)
}
