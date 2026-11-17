package payments

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrMissingPhone          = errors.New("phone number is required")
	ErrMissingReference      = errors.New("transaction reference is required")
	ErrAlreadyReconciled     = errors.New("transaction has already been reconciled")
	ErrInvalidTarget         = errors.New("invalid reconciliation target")
	ErrInvalidStatementFile  = errors.New("statement file could not be parsed")
)

type Service interface {
	InitiateSTKPush(ctx context.Context, businessID uuid.UUID, input STKPushInput) (*MpesaTransaction, error)
	HandleSTKCallback(signatureHeader string, body []byte) error
	HandleC2BValidation(signatureHeader string, req C2BValidationRequest) (C2BValidationResponse, error)
	HandleC2BConfirmation(signatureHeader string, req C2BConfirmationRequest) (C2BConfirmationResponse, error)
	InitiateB2C(ctx context.Context, businessID uuid.UUID, input B2CPayoutInput) (*MpesaTransaction, error)
	HandleB2CResult(signatureHeader string, body []byte) error
	HandleB2CTimeout(signatureHeader string, body []byte) error

	RecordManualBankTransfer(businessID uuid.UUID, input BankManualInput) (*BankTransaction, error)
	ImportBankStatement(businessID uuid.UUID, bankName string, r io.Reader) (*StatementImportResult, error)
	ListBankTransactions(businessID uuid.UUID, txType string, status string, page, limit int) ([]BankTransaction, int64, error)
	ListMpesaTransactions(businessID uuid.UUID, page, limit int) ([]MpesaTransaction, int64, error)

	ListReviewQueue(businessID uuid.UUID, source string, page, limit int) ([]ReviewQueueItem, int64, error)
	ReconcileReviewItem(businessID uuid.UUID, itemID uuid.UUID, input ReconcileInput) error
}

type service struct {
	repo         Repository
	mpesaClient  MpesaClient
	cfg          *config.Config
	invoicesSvc  invoices.Service
	customersSvc customers.Service
	financeSvc   finance.Service
}

func NewService(
	repo Repository,
	mpesaClient MpesaClient,
	cfg *config.Config,
	invoicesSvc invoices.Service,
	customersSvc customers.Service,
	financeSvc finance.Service,
) Service {
	return &service{
		repo:         repo,
		mpesaClient:  mpesaClient,
		cfg:          cfg,
		invoicesSvc:  invoicesSvc,
		customersSvc: customersSvc,
		financeSvc:   financeSvc,
	}
}

// InitiateSTKPush starts an M-Pesa STK push prompt with idempotency protection.
func (s *service) InitiateSTKPush(ctx context.Context, businessID uuid.UUID, input STKPushInput) (*MpesaTransaction, error) {
	if input.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	phone := NormalizePhone(input.PhoneNumber)
	if phone == "" {
		return nil, ErrMissingPhone
	}

	// Idempotency: return existing transaction if key was already used
	if input.IdempotencyKey != "" {
		if existing, err := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); err == nil && existing != nil {
			return existing, nil
		}
	}

	timestamp := time.Now().Format("20060102150405")
	shortCode := s.cfg.MpesaShortCode
	if shortCode == "" {
		shortCode = "174379" // Daraja test shortcode default
	}
	passkey := s.cfg.MpesaPasskey
	password := GeneratePassword(shortCode, passkey, timestamp)

	// Daraja API takes whole KSh units (minimum 1)
	kshAmount := input.Amount / 100
	if kshAmount <= 0 {
		kshAmount = 1
	}

	accountRef := strings.TrimSpace(input.AccountReference)
	if accountRef == "" {
		accountRef = "BusinessOS"
	}
	desc := strings.TrimSpace(input.TransactionDesc)
	if desc == "" {
		desc = "Invoice Payment"
	}

	callBackURL := s.cfg.MpesaStkCallbackURL
	if callBackURL == "" {
		callBackURL = fmt.Sprintf("%s/api/v1/payments/mpesa/callback/stk", s.cfg.FrontendURL)
	}

	darajaReq := DarajaSTKPushRequest{
		BusinessShortCode: shortCode,
		Password:          password,
		Timestamp:         timestamp,
		TransactionType:   "CustomerPayBillOnline",
		Amount:            kshAmount,
		PartyA:            phone,
		PartyB:            shortCode,
		PhoneNumber:       phone,
		CallBackURL:       callBackURL,
		AccountReference:  accountRef,
		TransactionDesc:   desc,
	}

	tx := &MpesaTransaction{
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		PhoneNumber:          phone,
		Amount:               input.Amount,
		Status:               StatusPending,
		IdempotencyKey:       input.IdempotencyKey,
		AccountReference:     accountRef,
		BillRefNumber:        accountRef,
		ReconciliationStatus: Unmatched,
	}

	// Make call to Daraja gateway
	resp, err := s.mpesaClient.InitiateSTKPush(ctx, darajaReq)
	if err != nil {
		// Even if gateway returned error, record failed transaction if we have response code
		if resp != nil {
			tx.MerchantRequestID = resp.MerchantRequestID
			tx.CheckoutRequestID = resp.CheckoutRequestID
			tx.Status = StatusFailed
			tx.ResultDesc = resp.ResponseDescription
			_ = s.repo.CreateMpesaTransaction(tx)
		}
		return nil, fmt.Errorf("initiate stk push: %w", err)
	}

	tx.MerchantRequestID = resp.MerchantRequestID
	tx.CheckoutRequestID = resp.CheckoutRequestID
	tx.ResultDesc = resp.ResponseDescription

	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		return nil, fmt.Errorf("save transaction: %w", err)
	}

	return tx, nil
}

// HandleSTKCallback processes M-Pesa STK push callback with signature and idempotency checks.
func (s *service) HandleSTKCallback(signatureHeader string, body []byte) error {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, body, signatureHeader) {
		return ErrInvalidSignature
	}

	var wrapper DarajaSTKCallbackWrapper
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return fmt.Errorf("decode callback body: %w", err)
	}
	cb := wrapper.Body.StkCallback

	tx, err := s.repo.GetMpesaTransactionByCheckoutRequestID(cb.CheckoutRequestID)
	if err != nil {
		return fmt.Errorf("transaction not found for checkout %s: %w", cb.CheckoutRequestID, err)
	}

	// Idempotency: skip if already settled
	if tx.Status == StatusCompleted || tx.Status == StatusFailed || tx.Status == StatusCancelled {
		return nil
	}

	tx.ResultCode = &cb.ResultCode
	tx.ResultDesc = cb.ResultDesc
	tx.RawPayload = string(body)

	if cb.ResultCode != 0 {
		if cb.ResultCode == 1032 { // Cancelled by user
			tx.Status = StatusCancelled
		} else {
			tx.Status = StatusFailed
		}
		tx.ReconciliationStatus = Ignored
		return s.repo.UpdateMpesaTransaction(tx)
	}

	tx.Status = StatusCompleted

	// Extract receipt and metadata
	if cb.CallbackMetadata != nil {
		for _, item := range cb.CallbackMetadata.Item {
			switch item.Name {
			case "MpesaReceiptNumber":
				if val, ok := item.Value.(string); ok {
					tx.MpesaReceiptNumber = val
				}
			case "PhoneNumber":
				tx.PhoneNumber = fmt.Sprintf("%v", item.Value)
			case "Amount":
				// If returned as float/int, ensure correct cents amount
				if f, ok := item.Value.(float64); ok {
					tx.Amount = int64(f * 100)
				}
			}
		}
	}

	// Automatic reconciliation: match to invoice or customer credit
	s.autoReconcileMpesa(tx)

	return s.repo.UpdateMpesaTransaction(tx)
}

// HandleC2BValidation validates incoming C2B payment before acceptance.
func (s *service) HandleC2BValidation(signatureHeader string, req C2BValidationRequest) (C2BValidationResponse, error) {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, nil, signatureHeader) {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: "Invalid signature"}, ErrInvalidSignature
	}

	amount, err := strconv.ParseFloat(req.TransAmount, 64)
	if err != nil || amount <= 0 {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: "Invalid amount"}, nil
	}

	return C2BValidationResponse{ResultCode: 0, ResultDesc: "Accepted"}, nil
}

// HandleC2BConfirmation handles confirmed C2B payment with automatic reconciliation.
func (s *service) HandleC2BConfirmation(signatureHeader string, req C2BConfirmationRequest) (C2BConfirmationResponse, error) {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, nil, signatureHeader) {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Invalid signature"}, ErrInvalidSignature
	}

	// Idempotency: skip duplicate TransID
	if existing, err := s.repo.GetMpesaTransactionByReceipt(req.TransID); err == nil && existing != nil {
		return C2BConfirmationResponse{ResultCode: 0, ResultDesc: "Accepted"}, nil
	}

	amountFloat, _ := strconv.ParseFloat(req.TransAmount, 64)
	amountCents := int64(amountFloat * 100)

	phone := NormalizePhone(req.MSISDN)
	billRef := strings.TrimSpace(req.BillRefNumber)
	if billRef == "" {
		billRef = strings.TrimSpace(req.InvoiceNumber)
	}

	// Resolve default business if single business, or extract business ID
	// For testing/mock, if not provided we fetch first or use dummy
	var businessID uuid.UUID
	// Try to find matching invoice across businesses or default
	if billRef != "" {
		if inv, err := s.repo.FindInvoiceByNumber(uuid.Nil, billRef); err == nil && inv != nil {
			businessID = inv.BusinessID
		}
	}
	if businessID == uuid.Nil && phone != "" {
		if cust, err := s.repo.FindCustomerByPhone(uuid.Nil, phone); err == nil && cust != nil {
			businessID = cust.BusinessID
		}
	}

	raw, _ := json.Marshal(req)
	tx := &MpesaTransaction{
		BusinessID:           businessID,
		TransactionType:      TypeC2B,
		MpesaReceiptNumber:   req.TransID,
		PhoneNumber:          phone,
		Amount:               amountCents,
		Status:               StatusCompleted,
		BillRefNumber:        billRef,
		AccountReference:     billRef,
		ReconciliationStatus: Unmatched,
		RawPayload:           string(raw),
	}

	if businessID != uuid.Nil {
		s.autoReconcileMpesa(tx)
	}

	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Failed to persist"}, err
	}

	return C2BConfirmationResponse{ResultCode: 0, ResultDesc: "Accepted"}, nil
}

// InitiateB2C starts an M-Pesa B2C payout for expenses or supplier settlements.
func (s *service) InitiateB2C(ctx context.Context, businessID uuid.UUID, input B2CPayoutInput) (*MpesaTransaction, error) {
	if input.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	phone := NormalizePhone(input.PhoneNumber)
	if phone == "" {
		return nil, ErrMissingPhone
	}

	if input.IdempotencyKey != "" {
		if existing, err := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); err == nil && existing != nil {
			return existing, nil
		}
	}

	shortCode := s.cfg.MpesaB2CShortCode
	if shortCode == "" {
		shortCode = s.cfg.MpesaShortCode
	}
	kshAmount := input.Amount / 100
	if kshAmount <= 0 {
		kshAmount = 1
	}

	darajaReq := DarajaB2CRequest{
		InitiatorName:      s.cfg.MpesaB2CInitiatorName,
		SecurityCredential: s.cfg.MpesaB2CSecurityCredential,
		CommandID:          "BusinessPayment",
		Amount:             kshAmount,
		PartyA:             shortCode,
		PartyB:             phone,
		Remarks:            input.Remarks,
		QueueTimeOutURL:    s.cfg.MpesaB2CTimeoutURL,
		ResultURL:          s.cfg.MpesaB2CResultURL,
		Occasion:           input.Occasion,
	}

	tx := &MpesaTransaction{
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		PhoneNumber:          phone,
		Amount:               input.Amount,
		Status:               StatusPending,
		IdempotencyKey:       input.IdempotencyKey,
		AccountReference:     input.Remarks,
		ReconciliationStatus: Unmatched,
	}

	resp, err := s.mpesaClient.InitiateB2CPayout(ctx, darajaReq)
	if err != nil {
		if resp != nil {
			tx.CheckoutRequestID = resp.ConversationID
			tx.Status = StatusFailed
			tx.ResultDesc = resp.ResponseDescription
			_ = s.repo.CreateMpesaTransaction(tx)
		}
		return nil, fmt.Errorf("initiate b2c: %w", err)
	}

	tx.CheckoutRequestID = resp.ConversationID
	tx.ResultDesc = resp.ResponseDescription

	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		return nil, fmt.Errorf("save transaction: %w", err)
	}

	return tx, nil
}

// HandleB2CResult processes the B2C callback from Safaricom.
func (s *service) HandleB2CResult(signatureHeader string, body []byte) error {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, body, signatureHeader) {
		return ErrInvalidSignature
	}

	var payload struct {
		Result struct {
			ResultCode     int    `json:"ResultCode"`
			ResultDesc     string `json:"ResultDesc"`
			ConversationID string `json:"ConversationID"`
			TransactionID  string `json:"TransactionID"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode b2c body: %w", err)
	}

	res := payload.Result
	tx, err := s.repo.GetMpesaTransactionByCheckoutRequestID(res.ConversationID)
	if err != nil {
		return fmt.Errorf("transaction not found for conversation %s: %w", res.ConversationID, err)
	}

	if tx.Status == StatusCompleted || tx.Status == StatusFailed {
		return nil
	}

	tx.ResultCode = &res.ResultCode
	tx.ResultDesc = res.ResultDesc
	tx.RawPayload = string(body)

	if res.ResultCode != 0 {
		tx.Status = StatusFailed
		tx.ReconciliationStatus = Ignored
	} else {
		tx.Status = StatusCompleted
		tx.MpesaReceiptNumber = res.TransactionID
		// Automatically reconcile as operating expense or supplier payment
		if s.financeSvc != nil {
			exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
				BusinessID:  tx.BusinessID,
				Category:    "Operations",
				Description: fmt.Sprintf("B2C payout %s (%s)", res.TransactionID, tx.PhoneNumber),
				Amount:      tx.Amount,
				IncurredAt:  time.Now(),
			})
			if err == nil && exp != nil {
				tx.ReconciliationStatus = Reconciled
				tx.ReconciledEntityType = EntityExpense
				tx.ReconciledEntityID = &exp.ID
			}
		}
	}

	return s.repo.UpdateMpesaTransaction(tx)
}

// HandleB2CTimeout processes queue timeout for B2C transactions.
func (s *service) HandleB2CTimeout(signatureHeader string, body []byte) error {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, body, signatureHeader) {
		return ErrInvalidSignature
	}

	var payload struct {
		Result struct {
			ConversationID string `json:"ConversationID"`
			ResultDesc     string `json:"ResultDesc"`
		} `json:"Result"`
	}
	_ = json.Unmarshal(body, &payload)
	if payload.Result.ConversationID != "" {
		if tx, err := s.repo.GetMpesaTransactionByCheckoutRequestID(payload.Result.ConversationID); err == nil && tx != nil {
			tx.Status = StatusFailed
			tx.ResultDesc = "Queue Timeout"
			tx.ReconciliationStatus = Ignored
			_ = s.repo.UpdateMpesaTransaction(tx)
		}
	}
	return nil
}

// autoReconcileMpesa attempts automatic matching against Invoices or Customer credit.
func (s *service) autoReconcileMpesa(tx *MpesaTransaction) {
	// 1. Try matching Invoice by AccountReference or BillRefNumber
	ref := strings.TrimSpace(tx.BillRefNumber)
	if ref == "" {
		ref = strings.TrimSpace(tx.AccountReference)
	}
	if ref != "" && s.invoicesSvc != nil {
		if inv, err := s.repo.FindInvoiceByNumber(tx.BusinessID, ref); err == nil && inv != nil && inv.Status != invoices.StatusPaid {
			_, err := s.invoicesSvc.RecordPayment(inv.ID, tx.BusinessID, invoices.RecordPaymentInput{
				Amount: tx.Amount,
				Note:   fmt.Sprintf("M-Pesa payment %s", tx.MpesaReceiptNumber),
			})
			if err == nil {
				tx.ReconciliationStatus = Reconciled
				tx.ReconciledEntityType = EntityInvoice
				tx.ReconciledEntityID = &inv.ID
				return
			}
		}
	}

	// 2. Try matching Customer by Phone Number
	if tx.PhoneNumber != "" && s.customersSvc != nil {
		if cust, err := s.repo.FindCustomerByPhone(tx.BusinessID, tx.PhoneNumber); err == nil && cust != nil {
			_, err := s.customersSvc.RecordPayment(cust.ID, tx.BusinessID, tx.Amount, fmt.Sprintf("M-Pesa receipt %s", tx.MpesaReceiptNumber))
			if err == nil {
				tx.ReconciliationStatus = Reconciled
				tx.ReconciledEntityType = EntityCustomerCredit
				tx.ReconciledEntityID = &cust.ID
				return
			}
		}
	}

	// Fallback: remains Unmatched and enters Review Queue
	tx.ReconciliationStatus = Unmatched
}

// RecordManualBankTransfer records a single bank transfer and attempts auto-reconciliation.
func (s *service) RecordManualBankTransfer(businessID uuid.UUID, input BankManualInput) (*BankTransaction, error) {
	ref := strings.TrimSpace(input.Reference)
	if ref == "" {
		return nil, ErrMissingReference
	}
	if input.Amount <= 0 {
		return nil, ErrInvalidAmount
	}

	// Check reference uniqueness per business
	if existing, err := s.repo.GetBankTransactionByReference(businessID, ref); err == nil && existing != nil {
		return nil, ErrDuplicateReference
	}

	transDate := input.TransactedAt
	if transDate.IsZero() {
		transDate = time.Now()
	}

	txType := BankInflow
	if strings.EqualFold(input.Type, "outflow") {
		txType = BankOutflow
	}

	tx := &BankTransaction{
		BusinessID:           businessID,
		BankName:             input.BankName,
		Reference:            ref,
		Type:                 txType,
		Amount:               input.Amount,
		Description:          input.Description,
		TransactedAt:         transDate,
		ReconciliationStatus: Unmatched,
	}

	// Automatic reconciliation matching on Reference and Description
	s.autoReconcileBank(tx)

	if err := s.repo.CreateBankTransaction(tx); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrDuplicateReference
		}
		return nil, err
	}

	return tx, nil
}

// autoReconcileBank matches bank inflows against Invoices and outflows against Expenses.
func (s *service) autoReconcileBank(tx *BankTransaction) {
	candidates := []string{strings.TrimSpace(tx.Reference), strings.TrimSpace(tx.Description)}
	for _, word := range strings.Fields(tx.Description) {
		clean := strings.Trim(word, ",.:;()")
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}
	for _, word := range strings.Fields(tx.Reference) {
		clean := strings.Trim(word, ",.:;()")
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}

	if tx.Type == BankInflow {
		// Attempt invoice match
		for _, token := range candidates {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if inv, err := s.repo.FindInvoiceByNumber(tx.BusinessID, token); err == nil && inv != nil && inv.Status != invoices.StatusPaid {
				if s.invoicesSvc != nil {
					_, err := s.invoicesSvc.RecordPayment(inv.ID, tx.BusinessID, invoices.RecordPaymentInput{
						Amount: tx.Amount,
						Note:   fmt.Sprintf("Bank transfer %s (%s)", tx.Reference, tx.BankName),
					})
					if err == nil {
						tx.ReconciliationStatus = Reconciled
						tx.ReconciledEntityType = EntityInvoice
						tx.ReconciledEntityID = &inv.ID
						return
					}
				}
			}
		}
	} else if tx.Type == BankOutflow && s.financeSvc != nil {
		// Outflow matched as expense
		exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
			BusinessID:  tx.BusinessID,
			Category:    "Operations",
			Description: fmt.Sprintf("Bank outflow %s: %s", tx.Reference, tx.Description),
			Amount:      tx.Amount,
			IncurredAt:  tx.TransactedAt,
		})
		if err == nil && exp != nil {
			tx.ReconciliationStatus = Reconciled
			tx.ReconciledEntityType = EntityExpense
			tx.ReconciledEntityID = &exp.ID
			return
		}
	}

	tx.ReconciliationStatus = Unmatched
}

// ImportBankStatement imports a CSV bank statement, skipping duplicate references and auto-reconciling.
func (s *service) ImportBankStatement(businessID uuid.UUID, bankName string, r io.Reader) (*StatementImportResult, error) {
	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, ErrInvalidStatementFile
	}

	headers := records[0]
	headerMap := make(map[string]int)
	for i, h := range headers {
		clean := strings.ToLower(strings.TrimSpace(h))
		headerMap[clean] = i
	}

	// Resolve column indices
	dateIdx := getHeaderIdx(headerMap, "date", "transacted_at", "transaction date")
	refIdx := getHeaderIdx(headerMap, "reference", "ref", "trans id", "reference number")
	descIdx := getHeaderIdx(headerMap, "description", "details", "narrative", "memo")
	amountIdx := getHeaderIdx(headerMap, "amount", "trans amount", "value")
	inflowIdx := getHeaderIdx(headerMap, "inflow", "credit", "deposit")
	outflowIdx := getHeaderIdx(headerMap, "outflow", "debit", "withdrawal")

	if refIdx == -1 {
		return nil, fmt.Errorf("%w: missing reference column", ErrInvalidStatementFile)
	}

	res := &StatementImportResult{TotalRows: len(records) - 1}

	for _, row := range records[1:] {
		if len(row) <= refIdx {
			continue
		}
		ref := strings.TrimSpace(row[refIdx])
		if ref == "" {
			continue
		}

		// Skip duplicate reference
		if existing, _ := s.repo.GetBankTransactionByReference(businessID, ref); existing != nil {
			res.SkippedDuplicates++
			continue
		}

		desc := ""
		if descIdx >= 0 && descIdx < len(row) {
			desc = strings.TrimSpace(row[descIdx])
		}

		var transDate time.Time
		if dateIdx >= 0 && dateIdx < len(row) {
			parsedDate, err := time.Parse("2006-01-02", strings.TrimSpace(row[dateIdx]))
			if err != nil {
				parsedDate, _ = time.Parse(time.RFC3339, strings.TrimSpace(row[dateIdx]))
			}
			transDate = parsedDate
		}
		if transDate.IsZero() {
			transDate = time.Now()
		}

		var amountCents int64
		var txType BankTransactionType = BankInflow

		if inflowIdx >= 0 && inflowIdx < len(row) && strings.TrimSpace(row[inflowIdx]) != "" {
			if v, err := parseMoneyString(row[inflowIdx]); err == nil && v > 0 {
				amountCents = v
				txType = BankInflow
			}
		}
		if outflowIdx >= 0 && outflowIdx < len(row) && strings.TrimSpace(row[outflowIdx]) != "" {
			if v, err := parseMoneyString(row[outflowIdx]); err == nil && v > 0 {
				amountCents = v
				txType = BankOutflow
			}
		}
		if amountCents == 0 && amountIdx >= 0 && amountIdx < len(row) {
			if v, err := parseMoneyString(row[amountIdx]); err == nil {
				if v < 0 {
					amountCents = -v
					txType = BankOutflow
				} else {
					amountCents = v
					txType = BankInflow
				}
			}
		}

		if amountCents <= 0 {
			continue
		}

		tx := &BankTransaction{
			BusinessID:           businessID,
			BankName:             bankName,
			Reference:            ref,
			Type:                 txType,
			Amount:               amountCents,
			Description:          desc,
			TransactedAt:         transDate,
			ReconciliationStatus: Unmatched,
		}

		s.autoReconcileBank(tx)

		if err := s.repo.CreateBankTransaction(tx); err == nil {
			res.Imported++
			if tx.ReconciliationStatus == Reconciled {
				res.Reconciled++
			} else {
				res.Unmatched++
			}
		}
	}

	return res, nil
}

func getHeaderIdx(m map[string]int, aliases ...string) int {
	for _, a := range aliases {
		if idx, ok := m[a]; ok {
			return idx
		}
	}
	return -1
}

func parseMoneyString(s string) (int64, error) {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "KSh", "")
	s = strings.ReplaceAll(s, "KES", "")
	s = strings.TrimSpace(s)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f * 100), nil
}

// ListBankTransactions returns paginated bank transactions.
func (s *service) ListBankTransactions(businessID uuid.UUID, txType string, status string, page, limit int) ([]BankTransaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	return s.repo.ListBankTransactions(businessID, txType, status, limit, offset)
}

// ListMpesaTransactions returns paginated M-Pesa transactions.
func (s *service) ListMpesaTransactions(businessID uuid.UUID, page, limit int) ([]MpesaTransaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	return s.repo.ListMpesaTransactions(businessID, limit, offset)
}

// ListReviewQueue returns unmatched items awaiting manual reconciliation.
func (s *service) ListReviewQueue(businessID uuid.UUID, source string, page, limit int) ([]ReviewQueueItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	return s.repo.ListReviewQueue(businessID, source, limit, offset)
}

// ReconcileReviewItem manually settles an unmatched transaction to an invoice, customer, or expense.
func (s *service) ReconcileReviewItem(businessID uuid.UUID, itemID uuid.UUID, input ReconcileInput) error {
	var (
		amount    int64
		refString string
	)

	// Fetch item according to source
	var mpesaTx *MpesaTransaction
	var bankTx *BankTransaction

	if input.Source == "mpesa" {
		tx, err := s.repo.GetMpesaTransactionByID(itemID, businessID)
		if err != nil {
			return ErrTransactionNotFound
		}
		if tx.ReconciliationStatus == Reconciled {
			return ErrAlreadyReconciled
		}
		mpesaTx = tx
		amount = tx.Amount
		refString = tx.MpesaReceiptNumber
	} else if input.Source == "bank" {
		tx, err := s.repo.GetBankTransactionByID(itemID, businessID)
		if err != nil {
			return ErrTransactionNotFound
		}
		if tx.ReconciliationStatus == Reconciled {
			return ErrAlreadyReconciled
		}
		bankTx = tx
		amount = tx.Amount
		refString = tx.Reference
	} else {
		return ErrInvalidTarget
	}

	note := input.Note
	if note == "" {
		note = fmt.Sprintf("Reconciled from %s (%s)", input.Source, refString)
	}

	switch input.TargetType {
	case "invoice":
		targetUUID, err := uuid.Parse(input.TargetID)
		if err != nil {
			return fmt.Errorf("invalid invoice id: %w", err)
		}
		if s.invoicesSvc == nil {
			return errors.New("invoice service not configured")
		}
		if _, err := s.invoicesSvc.RecordPayment(targetUUID, businessID, invoices.RecordPaymentInput{
			Amount: amount,
			Note:   note,
		}); err != nil {
			return fmt.Errorf("record invoice payment: %w", err)
		}
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Reconciled
			mpesaTx.ReconciledEntityType = EntityInvoice
			mpesaTx.ReconciledEntityID = &targetUUID
			return s.repo.UpdateMpesaTransaction(mpesaTx)
		}
		if bankTx != nil {
			bankTx.ReconciliationStatus = Reconciled
			bankTx.ReconciledEntityType = EntityInvoice
			bankTx.ReconciledEntityID = &targetUUID
			return s.repo.UpdateBankTransaction(bankTx)
		}

	case "customer_credit":
		targetUUID, err := uuid.Parse(input.TargetID)
		if err != nil {
			return fmt.Errorf("invalid customer id: %w", err)
		}
		if s.customersSvc == nil {
			return errors.New("customer service not configured")
		}
		if _, err := s.customersSvc.RecordPayment(targetUUID, businessID, amount, note); err != nil {
			return fmt.Errorf("record customer payment: %w", err)
		}
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Reconciled
			mpesaTx.ReconciledEntityType = EntityCustomerCredit
			mpesaTx.ReconciledEntityID = &targetUUID
			return s.repo.UpdateMpesaTransaction(mpesaTx)
		}
		if bankTx != nil {
			bankTx.ReconciliationStatus = Reconciled
			bankTx.ReconciledEntityType = EntityCustomerCredit
			bankTx.ReconciledEntityID = &targetUUID
			return s.repo.UpdateBankTransaction(bankTx)
		}

	case "expense":
		cat := strings.TrimSpace(input.ExpenseCategory)
		if cat == "" {
			cat = "Operations"
		}
		if s.financeSvc == nil {
			return errors.New("finance service not configured")
		}
		exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
			BusinessID:  businessID,
			Category:    cat,
			Description: note,
			Amount:      amount,
			IncurredAt:  time.Now(),
		})
		if err != nil {
			return fmt.Errorf("create expense: %w", err)
		}
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Reconciled
			mpesaTx.ReconciledEntityType = EntityExpense
			mpesaTx.ReconciledEntityID = &exp.ID
			return s.repo.UpdateMpesaTransaction(mpesaTx)
		}
		if bankTx != nil {
			bankTx.ReconciliationStatus = Reconciled
			bankTx.ReconciledEntityType = EntityExpense
			bankTx.ReconciledEntityID = &exp.ID
			return s.repo.UpdateBankTransaction(bankTx)
		}

	case "dismiss":
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Manual
			return s.repo.UpdateMpesaTransaction(mpesaTx)
		}
		if bankTx != nil {
			bankTx.ReconciliationStatus = Manual
			return s.repo.UpdateBankTransaction(bankTx)
		}

	default:
		return ErrInvalidTarget
	}

	return nil
}
