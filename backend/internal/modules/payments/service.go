package payments

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidAmount           = errors.New("amount must be greater than zero")
	ErrMissingPhone            = errors.New("phone number is required")
	ErrMissingReference        = errors.New("transaction reference is required")
	ErrAlreadyReconciled       = errors.New("transaction has already been reconciled")
	ErrTransactionNotCompleted = errors.New("only completed m-pesa transactions can be reconciled")
	ErrInvalidTarget           = errors.New("invalid reconciliation target")
	ErrTargetDirectionMismatch = errors.New("this reconciliation target does not match the payment direction")
	ErrSupplierNotFound        = errors.New("supplier not found")
	ErrInvalidStatementFile    = errors.New("statement file could not be parsed")
)

const (
	defaultExpenseCategory  = "Operations"
	queueTimeoutDescription = "Queue timeout - awaiting final result"
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
	suppliersSvc suppliers.Service
}

func NewService(
	repo Repository,
	mpesaClient MpesaClient,
	cfg *config.Config,
	invoicesSvc invoices.Service,
	customersSvc customers.Service,
	financeSvc finance.Service,
	suppliersSvc suppliers.Service,
) Service {
	return &service{
		repo:         repo,
		mpesaClient:  mpesaClient,
		cfg:          cfg,
		invoicesSvc:  invoicesSvc,
		customersSvc: customersSvc,
		financeSvc:   financeSvc,
		suppliersSvc: suppliersSvc,
	}
}

// backendOrigin is the public origin used to build provider callback URLs when
// no explicit URL is configured. BACKEND_URL wins over FRONTEND_URL so an API
// deployed on its own host still receives callbacks.
func (s *service) backendOrigin() string {
	if s.cfg == nil {
		return ""
	}
	base := strings.TrimSpace(s.cfg.BackendURL)
	if base == "" {
		base = strings.TrimSpace(s.cfg.FrontendURL)
	}
	if idx := strings.Index(base, ","); idx >= 0 {
		base = strings.TrimSpace(base[:idx])
	}
	return strings.TrimRight(base, "/")
}

// callbackURL returns the configured callback URL, or builds one from the API
// origin. When a callback secret is configured, the token is embedded in the
// query string because Daraja cannot send custom headers.
func (s *service) callbackURL(configured, path string) string {
	raw := strings.TrimSpace(configured)
	if raw == "" {
		raw = s.backendOrigin() + path
	}
	parsed, err := url.Parse(raw)
	if err != nil || s.cfg == nil || s.cfg.MpesaCallbackSecret == "" {
		return raw
	}
	query := parsed.Query()
	if query.Get("token") == "" {
		query.Set("token", s.cfg.MpesaCallbackSecret)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
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

	callBackURL := s.callbackURL(s.cfg.MpesaStkCallbackURL, "/api/v1/payments/mpesa/callback/stk")

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

	// Idempotency: a terminal status means another delivery already settled
	// this transaction, so replaying the financial effect is never safe.
	if tx.Status.IsTerminal() {
		return nil
	}

	tx.ResultCode = &cb.ResultCode
	tx.ResultDesc = cb.ResultDesc
	tx.RawPayload = string(body)

	terminal := StatusCompleted
	if cb.ResultCode != 0 {
		terminal = StatusFailed
		if cb.ResultCode == 1032 { // Cancelled by user
			terminal = StatusCancelled
		}
	}

	if terminal == StatusCompleted {
		// Extract receipt and metadata before claiming so the durable row has
		// everything needed to reconcile later.
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
	}

	// Persist the terminal status before touching invoices or customer credit.
	// This is the compare-and-set that makes a redelivered callback a no-op and
	// closes the double-apply window reported on the original ordering.
	claimed, err := s.repo.ClaimMpesaTransactionStatus(tx, terminal, StatusPending)
	if err != nil {
		return fmt.Errorf("save callback status: %w", err)
	}
	if !claimed {
		// A concurrent delivery won the transition and owns the side effect.
		return nil
	}

	tx.Status = terminal
	if terminal != StatusCompleted {
		tx.ReconciliationStatus = Ignored
		return s.repo.UpdateMpesaReconciliation(tx)
	}

	// Automatic reconciliation: match to invoice or customer credit. A failure
	// here leaves the transaction completed and unmatched, so it surfaces in the
	// review queue without ever replaying the payment.
	if err := s.autoReconcileMpesa(tx); err != nil {
		log.Printf("payments: stk reconciliation for transaction %s failed: %v", tx.ID, err)
		return nil
	}

	return s.repo.UpdateMpesaReconciliation(tx)
}

// HandleC2BValidation validates incoming C2B payment before acceptance.
func (s *service) HandleC2BValidation(signatureHeader string, req C2BValidationRequest) (C2BValidationResponse, error) {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, nil, signatureHeader) {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: DescInvalidSignature}, ErrInvalidSignature
	}

	amount, err := strconv.ParseFloat(req.TransAmount, 64)
	if err != nil || amount <= 0 {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: "Invalid amount"}, nil
	}

	return C2BValidationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
}

// HandleC2BConfirmation handles confirmed C2B payment with automatic reconciliation.
func (s *service) HandleC2BConfirmation(signatureHeader string, req C2BConfirmationRequest) (C2BConfirmationResponse, error) {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, nil, signatureHeader) {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: DescInvalidSignature}, ErrInvalidSignature
	}

	// Idempotency: skip duplicate TransID
	if existing, err := s.repo.GetMpesaTransactionByReceipt(req.TransID); err == nil && existing != nil {
		return C2BConfirmationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
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
	if businessID == uuid.Nil {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Could not resolve business"}, fmt.Errorf("resolve business for c2b payment %s: %w", req.TransID, ErrTransactionNotFound)
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

	// Persist the receipt before applying the payment. The unique receipt lookup
	// above then makes a redelivered C2B confirmation a no-op even if
	// reconciliation failed on the first attempt.
	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Failed to persist"}, err
	}

	if err := s.autoReconcileMpesa(tx); err != nil {
		log.Printf("payments: c2b reconciliation for transaction %s failed: %v", tx.ID, err)
		return C2BConfirmationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
	}
	if err := s.repo.UpdateMpesaReconciliation(tx); err != nil {
		log.Printf("payments: could not save c2b reconciliation for transaction %s: %v", tx.ID, err)
	}

	return C2BConfirmationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
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

	// A payout to a supplier must remember its target so the result callback
	// settles the payable instead of recording a generic expense.
	var supplierID *uuid.UUID
	if raw := strings.TrimSpace(input.SupplierID); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, ErrInvalidTarget
		}
		if s.suppliersSvc == nil {
			return nil, errors.New("supplier service not configured")
		}
		if _, err := s.suppliersSvc.Get(parsed, businessID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, suppliers.ErrSupplierNotFound) {
				return nil, ErrSupplierNotFound
			}
			return nil, err
		}
		supplierID = &parsed
	}

	expenseCategory := strings.TrimSpace(input.ExpenseCat)
	if expenseCategory == "" {
		expenseCategory = defaultExpenseCategory
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
		QueueTimeOutURL:    s.callbackURL(s.cfg.MpesaB2CTimeoutURL, "/api/v1/payments/mpesa/callback/b2c-timeout"),
		ResultURL:          s.callbackURL(s.cfg.MpesaB2CResultURL, "/api/v1/payments/mpesa/callback/b2c-result"),
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
		SupplierID:           supplierID,
		ExpenseCategory:      expenseCategory,
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

	if tx.Status.IsTerminal() {
		return nil
	}

	tx.ResultCode = &res.ResultCode
	tx.ResultDesc = res.ResultDesc
	tx.RawPayload = string(body)

	terminal := StatusFailed
	if res.ResultCode == 0 {
		terminal = StatusCompleted
		tx.MpesaReceiptNumber = res.TransactionID
	}

	// A queue timeout is not final, so a real result may resolve it. Only a
	// pending or timed-out transaction can be claimed; a completed payout is
	// never downgraded by a late timeout or duplicate result.
	claimed, err := s.repo.ClaimMpesaTransactionStatus(tx, terminal, StatusPending, StatusTimedOut)
	if err != nil {
		return fmt.Errorf("save b2c result: %w", err)
	}
	if !claimed {
		return nil
	}

	tx.Status = terminal
	if terminal == StatusFailed {
		tx.ReconciliationStatus = Ignored
		return s.repo.UpdateMpesaReconciliation(tx)
	}

	// Claiming completion before creating the expense or supplier payment means
	// a redelivered callback cannot pay the same supplier twice.
	if err := s.applyB2CPayout(tx); err != nil {
		log.Printf("payments: b2c reconciliation for transaction %s failed: %v", tx.ID, err)
		return nil
	}

	return s.repo.UpdateMpesaReconciliation(tx)
}

// applyB2CPayout records the financial effect of a completed B2C payout. It
// settles the supplier payable when the payout had a supplier target, and
// otherwise records an operating expense.
func (s *service) applyB2CPayout(tx *MpesaTransaction) error {
	marker := mpesaMarker(tx)

	if tx.SupplierID != nil {
		if s.suppliersSvc == nil {
			return errors.New("supplier service not configured")
		}
		if s.supplierPaymentExists(*tx.SupplierID, tx.BusinessID, marker) {
			tx.ReconciliationStatus = Reconciled
			tx.ReconciledEntityType = EntitySupplierPayment
			tx.ReconciledEntityID = tx.SupplierID
			return nil
		}
		if _, err := s.suppliersSvc.RecordPayment(*tx.SupplierID, tx.BusinessID, tx.Amount, marker+" supplier payout"); err != nil {
			return fmt.Errorf("record supplier payment: %w", err)
		}
		tx.ReconciliationStatus = Reconciled
		tx.ReconciledEntityType = EntitySupplierPayment
		tx.ReconciledEntityID = tx.SupplierID
		return nil
	}

	if s.financeSvc == nil {
		return errors.New("finance service not configured")
	}
	if existing := s.findMpesaExpense(tx, marker); existing != nil {
		tx.ReconciliationStatus = Reconciled
		tx.ReconciledEntityType = EntityExpense
		tx.ReconciledEntityID = &existing.ID
		return nil
	}
	category := strings.TrimSpace(tx.ExpenseCategory)
	if category == "" {
		category = defaultExpenseCategory
	}
	exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
		BusinessID:  tx.BusinessID,
		Category:    category,
		Description: fmt.Sprintf("%s payout (%s)", marker, tx.PhoneNumber),
		Amount:      tx.Amount,
		IncurredAt:  time.Now(),
	})
	if err != nil {
		return fmt.Errorf("create expense: %w", err)
	}
	if exp != nil {
		tx.ReconciliationStatus = Reconciled
		tx.ReconciledEntityType = EntityExpense
		tx.ReconciledEntityID = &exp.ID
	}
	return nil
}

// HandleB2CTimeout processes queue timeout for B2C transactions. A timeout is
// ambiguous, so it becomes StatusTimedOut and never overwrites a completed
// payout. Any later result callback may still settle the transaction.
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
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode b2c timeout body: %w", err)
	}
	if strings.TrimSpace(payload.Result.ConversationID) == "" {
		return fmt.Errorf("b2c timeout: missing conversation id: %w", ErrMissingReference)
	}

	tx, err := s.repo.GetMpesaTransactionByCheckoutRequestID(payload.Result.ConversationID)
	if err != nil {
		return fmt.Errorf("transaction not found for conversation %s: %w", payload.Result.ConversationID, err)
	}

	tx.ResultCode = nil
	tx.ResultDesc = queueTimeoutDescription
	tx.RawPayload = string(body)

	claimed, err := s.repo.ClaimMpesaTransactionStatus(tx, StatusTimedOut, StatusPending)
	if err != nil {
		return fmt.Errorf("save b2c timeout: %w", err)
	}
	if !claimed {
		// The transaction already reached a final state (or timed out twice).
		return nil
	}
	tx.Status = StatusTimedOut
	return nil
}

// autoReconcileMpesa attempts automatic matching against invoices or customer
// credit. It returns an error when a match was found but the payment could not
// be recorded, leaving the caller to surface the transaction for review.
func (s *service) autoReconcileMpesa(tx *MpesaTransaction) error {
	marker := mpesaMarker(tx)

	// 1. Try matching Invoice by AccountReference or BillRefNumber
	ref := strings.TrimSpace(tx.BillRefNumber)
	if ref == "" {
		ref = strings.TrimSpace(tx.AccountReference)
	}
	if ref != "" && s.invoicesSvc != nil {
		if inv, err := s.repo.FindInvoiceByNumber(tx.BusinessID, ref); err == nil && inv != nil && inv.Status != invoices.StatusPaid {
			if !s.invoicePaymentExists(inv.ID, tx.BusinessID, marker) {
				_, err := s.invoicesSvc.RecordPayment(inv.ID, tx.BusinessID, invoices.RecordPaymentInput{
					Amount: tx.Amount,
					Note:   marker + " invoice payment",
				})
				if err != nil {
					return err
				}
			}
			tx.ReconciliationStatus = Reconciled
			tx.ReconciledEntityType = EntityInvoice
			tx.ReconciledEntityID = &inv.ID
			return nil
		}
	}

	// 2. Try matching Customer by Phone Number
	if tx.PhoneNumber != "" && s.customersSvc != nil {
		if cust, err := s.repo.FindCustomerByPhone(tx.BusinessID, tx.PhoneNumber); err == nil && cust != nil {
			if !s.customerPaymentExists(cust.ID, tx.BusinessID, marker) {
				if _, err := s.customersSvc.RecordPayment(cust.ID, tx.BusinessID, tx.Amount, marker+" customer payment"); err != nil {
					return err
				}
			}
			tx.ReconciliationStatus = Reconciled
			tx.ReconciledEntityType = EntityCustomerCredit
			tx.ReconciledEntityID = &cust.ID
			return nil
		}
	}

	// Fallback: remains Unmatched and enters Review Queue
	tx.ReconciliationStatus = Unmatched
	return nil
}

// mpesaMarker is the stable prefix that identifies payments applied for one
// M-Pesa transaction. Dedupe checks use it so a retried reconciliation can
// never record the same money twice.
func mpesaMarker(tx *MpesaTransaction) string {
	ref := strings.TrimSpace(tx.MpesaReceiptNumber)
	if ref == "" {
		ref = tx.ID.String()
	}
	return "M-Pesa " + ref
}

func (s *service) invoicePaymentExists(invoiceID, businessID uuid.UUID, marker string) bool {
	if s.invoicesSvc == nil {
		return false
	}
	payments, err := s.invoicesSvc.ListPayments(invoiceID, businessID)
	if err != nil {
		return false
	}
	for _, payment := range payments {
		if strings.HasPrefix(payment.Note, marker) {
			return true
		}
	}
	return false
}

func (s *service) customerPaymentExists(customerID, businessID uuid.UUID, marker string) bool {
	if s.customersSvc == nil {
		return false
	}
	payments, err := s.customersSvc.ListPayments(customerID, businessID)
	if err != nil {
		return false
	}
	for _, payment := range payments {
		if strings.HasPrefix(payment.Note, marker) {
			return true
		}
	}
	return false
}

func (s *service) supplierPaymentExists(supplierID, businessID uuid.UUID, marker string) bool {
	if s.suppliersSvc == nil {
		return false
	}
	payments, err := s.suppliersSvc.ListPayments(supplierID, businessID)
	if err != nil {
		return false
	}
	for _, payment := range payments {
		if strings.HasPrefix(payment.Note, marker) {
			return true
		}
	}
	return false
}

// findMpesaExpense checks a bounded window around the transaction for an
// expense already created for this payout.
func (s *service) findMpesaExpense(tx *MpesaTransaction, marker string) *finance.Expense {
	if s.financeSvc == nil {
		return nil
	}
	from := tx.CreatedAt.Add(-24 * time.Hour)
	to := time.Now().Add(24 * time.Hour)
	if tx.CreatedAt.IsZero() {
		from = time.Now().Add(-365 * 24 * time.Hour)
	}
	expenses, err := s.financeSvc.ListExpenses(tx.BusinessID, from, to)
	if err != nil {
		return nil
	}
	for i := range expenses {
		if strings.HasPrefix(expenses[i].Description, marker) {
			return &expenses[i]
		}
	}
	return nil
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
		txType := BankInflow

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

// ReconcileReviewItem manually settles an unmatched transaction. Inflows may
// only settle invoices or customer credit; outflows may only record an expense.
func (s *service) ReconcileReviewItem(businessID uuid.UUID, itemID uuid.UUID, input ReconcileInput) error {
	var (
		amount    int64
		refString string
		direction string
	)

	// Fetch item according to source
	var mpesaTx *MpesaTransaction
	var bankTx *BankTransaction

	switch input.Source {
	case "mpesa":
		tx, err := s.repo.GetMpesaTransactionByID(itemID, businessID)
		if err != nil {
			return ErrTransactionNotFound
		}
		if tx.ReconciliationStatus == Reconciled {
			return ErrAlreadyReconciled
		}
		if tx.Status != StatusCompleted {
			return ErrTransactionNotCompleted
		}
		mpesaTx = tx
		amount = tx.Amount
		refString = tx.MpesaReceiptNumber
		direction = string(BankInflow)
		if tx.TransactionType == TypeB2C {
			direction = string(BankOutflow)
		}
	case "bank":
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
		direction = string(tx.Type)
	default:
		return ErrInvalidTarget
	}

	// Money that left the business cannot settle a receivable, and money that
	// arrived cannot be booked as an expense.
	switch input.TargetType {
	case "invoice", "customer_credit":
		if direction == string(BankOutflow) {
			return ErrTargetDirectionMismatch
		}
	case "expense":
		if direction == string(BankInflow) {
			return ErrTargetDirectionMismatch
		}
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
		paymentNote := note
		if mpesaTx != nil {
			marker := mpesaMarker(mpesaTx)
			paymentNote = marker + " invoice payment: " + note
			if !s.invoicePaymentExists(targetUUID, businessID, marker) {
				if _, err := s.invoicesSvc.RecordPayment(targetUUID, businessID, invoices.RecordPaymentInput{
					Amount: amount,
					Note:   paymentNote,
				}); err != nil {
					return fmt.Errorf("record invoice payment: %w", err)
				}
			}
		} else if _, err := s.invoicesSvc.RecordPayment(targetUUID, businessID, invoices.RecordPaymentInput{
			Amount: amount,
			Note:   paymentNote,
		}); err != nil {
			return fmt.Errorf("record invoice payment: %w", err)
		}
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Reconciled
			mpesaTx.ReconciledEntityType = EntityInvoice
			mpesaTx.ReconciledEntityID = &targetUUID
			return s.repo.UpdateMpesaReconciliation(mpesaTx)
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
		paymentNote := note
		if mpesaTx != nil {
			marker := mpesaMarker(mpesaTx)
			paymentNote = marker + " customer payment: " + note
			if !s.customerPaymentExists(targetUUID, businessID, marker) {
				if _, err := s.customersSvc.RecordPayment(targetUUID, businessID, amount, paymentNote); err != nil {
					return fmt.Errorf("record customer payment: %w", err)
				}
			}
		} else if _, err := s.customersSvc.RecordPayment(targetUUID, businessID, amount, paymentNote); err != nil {
			return fmt.Errorf("record customer payment: %w", err)
		}
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Reconciled
			mpesaTx.ReconciledEntityType = EntityCustomerCredit
			mpesaTx.ReconciledEntityID = &targetUUID
			return s.repo.UpdateMpesaReconciliation(mpesaTx)
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
			cat = defaultExpenseCategory
		}
		if s.financeSvc == nil {
			return errors.New("finance service not configured")
		}
		description := note
		var existing *finance.Expense
		if mpesaTx != nil {
			marker := mpesaMarker(mpesaTx)
			description = marker + " payout: " + note
			existing = s.findMpesaExpense(mpesaTx, marker)
		}
		var expenseID uuid.UUID
		if existing != nil {
			expenseID = existing.ID
		} else {
			exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
				BusinessID:  businessID,
				Category:    cat,
				Description: description,
				Amount:      amount,
				IncurredAt:  time.Now(),
			})
			if err != nil {
				return fmt.Errorf("create expense: %w", err)
			}
			expenseID = exp.ID
		}
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Reconciled
			mpesaTx.ReconciledEntityType = EntityExpense
			mpesaTx.ReconciledEntityID = &expenseID
			return s.repo.UpdateMpesaReconciliation(mpesaTx)
		}
		if bankTx != nil {
			bankTx.ReconciliationStatus = Reconciled
			bankTx.ReconciledEntityType = EntityExpense
			bankTx.ReconciledEntityID = &expenseID
			return s.repo.UpdateBankTransaction(bankTx)
		}

	case "dismiss":
		if mpesaTx != nil {
			mpesaTx.ReconciliationStatus = Manual
			return s.repo.UpdateMpesaReconciliation(mpesaTx)
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
