package payments

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/permissions"
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

func extractSignature(c *gin.Context) string {
	sig := c.GetHeader("X-Signature")
	if sig == "" {
		sig = c.GetHeader("X-Callback-Signature")
	}
	if sig == "" {
		sig = c.GetHeader("X-Mpesa-Signature")
	}
	if sig == "" {
		sig = c.Query("token")
	}
	return sig
}

const (
	maxCallbackBodyBytes  = 1 << 20  // 1 MB
	maxStatementBodyBytes = 10 << 20 // 10 MB
)

func parsePagination(c *gin.Context) (page int, limit int) {
	var err error
	page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err = strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}
	return page, limit
}

// Public Webhook Handlers

func (h *Handler) HandleSTKCallback(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBodyBytes)
	sig := extractSignature(c)
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{ResultCodeKey: 1, ResultDescKey: DescInvalidPayload})
		return
	}

	if err := h.service.HandleSTKCallback(sig, body); err != nil {
		if errors.Is(err, ErrInvalidSignature) {
			c.JSON(http.StatusUnauthorized, gin.H{ResultCodeKey: 1, ResultDescKey: DescInvalidSignature})
			return
		}
		c.JSON(http.StatusOK, gin.H{ResultCodeKey: 1, ResultDescKey: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{ResultCodeKey: 0, ResultDescKey: DescAccepted})
}

func (h *Handler) HandleC2BValidate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBodyBytes)
	sig := extractSignature(c)
	// Read the raw body so a sender that signs the body can be authenticated,
	// not just deployments that use the URL token.
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusOK, C2BValidationResponse{ResultCode: 1, ResultDesc: DescInvalidPayload})
		return
	}
	var req C2BValidationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusOK, C2BValidationResponse{ResultCode: 1, ResultDesc: DescInvalidJSON})
		return
	}

	res, err := h.service.HandleC2BValidation(sig, body, req)
	if err != nil && errors.Is(err, ErrInvalidSignature) {
		c.JSON(http.StatusUnauthorized, res)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) HandleC2BConfirm(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBodyBytes)
	sig := extractSignature(c)
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusOK, C2BConfirmationResponse{ResultCode: 1, ResultDesc: DescInvalidPayload})
		return
	}
	var req C2BConfirmationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusOK, C2BConfirmationResponse{ResultCode: 1, ResultDesc: DescInvalidJSON})
		return
	}

	// C2B confirmations are called by Safaricom without a user JWT, so a
	// missing business context is expected; the service resolves the tenant.
	businessID, _ := middleware.CurrentBusinessID(c)

	res, err := h.service.HandleC2BConfirmation(sig, body, req, businessID)
	if err != nil && errors.Is(err, ErrInvalidSignature) {
		c.JSON(http.StatusUnauthorized, res)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) HandleB2CResult(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBodyBytes)
	sig := extractSignature(c)
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{ResultCodeKey: 1, ResultDescKey: DescInvalidPayload})
		return
	}

	if err := h.service.HandleB2CResult(sig, body); err != nil {
		if errors.Is(err, ErrInvalidSignature) {
			c.JSON(http.StatusUnauthorized, gin.H{ResultCodeKey: 1, ResultDescKey: DescInvalidSignature})
			return
		}
		c.JSON(http.StatusOK, gin.H{ResultCodeKey: 1, ResultDescKey: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{ResultCodeKey: 0, ResultDescKey: DescAccepted})
}

func (h *Handler) HandleB2CTimeout(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBodyBytes)
	sig := extractSignature(c)
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{ResultCodeKey: 1, ResultDescKey: DescInvalidPayload})
		return
	}
	if err := h.service.HandleB2CTimeout(sig, body); err != nil {
		if errors.Is(err, ErrInvalidSignature) {
			c.JSON(http.StatusUnauthorized, gin.H{ResultCodeKey: 1, ResultDescKey: DescInvalidSignature})
			return
		}
		c.JSON(http.StatusOK, gin.H{ResultCodeKey: 1, ResultDescKey: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{ResultCodeKey: 0, ResultDescKey: DescAccepted})
}

// Protected Endpoints

func (h *Handler) InitiateSTKPush(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var input STKPushInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.service.InitiateSTKPush(c.Request.Context(), businessID, input)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, http.StatusOK, tx)
}

func (h *Handler) InitiateB2C(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var input B2CPayoutInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// A supplier-targeted payout settles a supplier payable, so it must require
	// the SupplierPayment capability in addition to PaymentManage. A payout
	// without a supplier target posts an operating expense when it completes,
	// so it equally requires FinanceManage.
	role, _ := c.Get(middleware.ContextRoleKey)
	roleStr, _ := role.(string)
	if strings.TrimSpace(input.SupplierID) != "" {
		if !permissions.Allows(roleStr, permissions.SupplierPayment) {
			response.Error(c, http.StatusForbidden, "insufficient permissions")
			return
		}
	} else if !permissions.Allows(roleStr, permissions.FinanceManage) {
		response.Error(c, http.StatusForbidden, "insufficient permissions")
		return
	}

	tx, err := h.service.InitiateB2C(c.Request.Context(), businessID, input)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, http.StatusOK, tx)
}

func (h *Handler) ListMpesaTransactions(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	page, limit := parsePagination(c)

	list, total, err := h.service.ListMpesaTransactions(businessID, page, limit)
	if err != nil {
		if errors.Is(err, ErrPageOutOfBounds) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Paginated(c, http.StatusOK, list, page, limit, total)
}

func (h *Handler) RecordManualBank(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var input BankManualInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// Automatic outflow matching can create an expense, which needs the same
	// FinanceManage capability as the explicit expense endpoint. Callers
	// without it may still record the transfer; it stays unmatched for review.
	role, _ := c.Get(middleware.ContextRoleKey)
	roleStr, _ := role.(string)
	canPostExpenses := permissions.Allows(roleStr, permissions.FinanceManage)

	tx, err := h.service.RecordManualBankTransfer(businessID, input, canPostExpenses)
	if err != nil {
		if errors.Is(err, ErrDuplicateReference) {
			response.Error(c, http.StatusConflict, err.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, http.StatusCreated, tx)
}

func (h *Handler) ImportBankStatement(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxStatementBodyBytes)
	bankName := c.DefaultPostForm("bank_name", c.Query("bank_name"))

	var r io.Reader
	file, _, err := c.Request.FormFile("statement")
	if err == nil {
		defer func() { _ = file.Close() }()
		r = file
	} else {
		// Try raw body if multipart form-data was not used
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil || len(bodyBytes) == 0 {
			response.Error(c, http.StatusBadRequest, "no file or CSV content provided")
			return
		}
		r = strings.NewReader(string(bodyBytes))
	}

	// Imported rows have no explicit settlement intent, so outflows are left
	// unmatched for review rather than auto-posted as expenses.
	result, err := h.service.ImportBankStatement(businessID, bankName, r)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, http.StatusOK, result)
}

func (h *Handler) ListBankTransactions(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	page, limit := parsePagination(c)
	txType := c.Query("type")
	status := c.Query("status")

	list, total, err := h.service.ListBankTransactions(businessID, txType, status, page, limit)
	if err != nil {
		if errors.Is(err, ErrPageOutOfBounds) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Paginated(c, http.StatusOK, list, page, limit, total)
}

func (h *Handler) ListReviewQueue(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	source := c.Query("source")
	page, limit := parsePagination(c)

	items, total, err := h.service.ListReviewQueue(businessID, source, page, limit)
	if err != nil {
		if errors.Is(err, ErrPageOutOfBounds) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Paginated(c, http.StatusOK, items, page, limit, total)
}

func (h *Handler) ReconcileReviewItem(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	rawID := c.Param("id")
	itemID, err := uuid.Parse(rawID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid item id")
		return
	}

	var input ReconcileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// Some review targets create ledger entries that normally require a
	// stronger permission than the route-level PaymentManage. Check the target
	// before dispatching so a cashier cannot create expenses or supplier
	// payments through the review queue.
	role, _ := c.Get(middleware.ContextRoleKey)
	roleStr, _ := role.(string)
	switch input.TargetType {
	case "expense":
		if !permissions.Allows(roleStr, permissions.FinanceManage) {
			response.Error(c, http.StatusForbidden, "insufficient permissions")
			return
		}
	case "supplier_payment":
		if !permissions.Allows(roleStr, permissions.SupplierPayment) {
			response.Error(c, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	if err := h.service.ReconcileReviewItem(businessID, itemID, input); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, http.StatusOK, gin.H{"reconciled": true})
}
