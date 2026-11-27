package payments

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
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
	ErrIdempotencyKeyConflict  = errors.New("idempotency key already used for different request")
	ErrBusinessNotBound        = errors.New("business is not authorized to use deployment daraja account")
)

const (
	defaultExpenseCategory  = "Operations"
	queueTimeoutDescription = "Queue timeout - awaiting final result"
)

type Service interface {
	InitiateSTKPush(ctx context.Context, businessID uuid.UUID, input STKPushInput) (*MpesaTransaction, error)
	HandleSTKCallback(signatureHeader string, body []byte) error
	HandleC2BValidation(signatureHeader string, req C2BValidationRequest) (C2BValidationResponse, error)
	HandleC2BConfirmation(signatureHeader string, req C2BConfirmationRequest, businessID uuid.UUID) (C2BConfirmationResponse, error)
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
	// Always write the current secret so a configured URL that carries a stale
	// token is corrected rather than left to fail signature validation.
	query.Set("token", s.cfg.MpesaCallbackSecret)
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

	// Idempotency: return existing transaction if key was already used for the same request
	if input.IdempotencyKey != "" {
		if existing, err := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); err == nil && existing != nil {
			if existing.TransactionType != TypeSTKPush || existing.Amount != input.Amount || existing.PhoneNumber != phone {
				return nil, ErrIdempotencyKeyConflict
			}
			// Do not return a failed transaction as a successful replay.
			if existing.Status == StatusFailed {
				return nil, ErrIdempotencyKeyConflict
			}
			return existing, nil
		}
	}

	if s.cfg != nil && s.cfg.MpesaBusinessID != "" {
		if boundID, err := uuid.Parse(s.cfg.MpesaBusinessID); err == nil && boundID != businessID {
			return nil, ErrBusinessNotBound
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

	// Persist pending transaction before gateway call to reserve idempotency key
	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		if input.IdempotencyKey != "" {
			if existing, lookupErr := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); lookupErr == nil && existing != nil {
				// The concurrent winner must describe the same request, otherwise
				// a different payment could be silently returned.
				if existing.TransactionType != TypeSTKPush || existing.Amount != input.Amount || existing.PhoneNumber != phone {
					return nil, ErrIdempotencyKeyConflict
				}
				return existing, nil
			}
		}
		return nil, fmt.Errorf("create pending transaction: %w", err)
	}

	// Make call to Daraja gateway
	resp, err := s.mpesaClient.InitiateSTKPush(ctx, darajaReq)
	if err != nil {
		if errors.Is(err, ErrMpesaPreDispatch) {
			// Proven pre-dispatch failure: release the reservation so a retry can
			// dispatch instead of replaying a stuck pending row.
			if delErr := s.repo.DeleteMpesaTransaction(tx.ID); delErr != nil {
				log.Printf("payments: release stk reservation %s failed: %v", tx.ID, delErr)
			}
			return nil, fmt.Errorf("initiate stk push: %w", err)
		}
		if resp != nil {
			tx.MerchantRequestID = resp.MerchantRequestID
			tx.CheckoutRequestID = resp.CheckoutRequestID
			tx.ResultDesc = resp.ResponseDescription
			if errors.Is(err, ErrTransactionFailed) || (resp.ResponseCode != "" && resp.ResponseCode != "0") {
				tx.Status = StatusFailed
			}
			if saveErr := s.repo.UpdateMpesaTransaction(tx); saveErr != nil {
				log.Printf("payments: save stk failure for %s failed: %v", tx.ID, saveErr)
			}
		}
		return nil, fmt.Errorf("initiate stk push: %w", err)
	}

	tx.MerchantRequestID = resp.MerchantRequestID
	tx.CheckoutRequestID = resp.CheckoutRequestID
	tx.ResultDesc = resp.ResponseDescription

	if err := s.repo.UpdateMpesaTransaction(tx); err != nil {
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

	// A signed callback without an explicit ResultCode is not success; decoding
	// a missing integer would otherwise default to 0.
	if cb.ResultCode == nil {
		return fmt.Errorf("stk callback missing ResultCode")
	}

	tx, err := s.repo.GetMpesaTransactionByCheckoutRequestID(cb.CheckoutRequestID)
	if err != nil {
		return fmt.Errorf("transaction not found for checkout %s: %w", cb.CheckoutRequestID, err)
	}

	// Idempotency: a terminal status means another delivery already settled
	// this transaction, so replaying the financial effect is never safe.
	if tx.Status.IsTerminal() {
		return nil
	}

	tx.ResultCode = cb.ResultCode
	tx.ResultDesc = cb.ResultDesc
	tx.RawPayload = string(body)

	terminal := StatusCompleted
	if *cb.ResultCode != 0 {
		terminal = StatusFailed
		if *cb.ResultCode == 1032 { // Cancelled by user
			terminal = StatusCancelled
		}
	}

	if terminal == StatusCompleted {
		// Extract receipt and metadata before claiming so the durable row has
		// everything needed to reconcile later.
		var confirmedReceipt string
		var confirmedAmount int64
		if cb.CallbackMetadata != nil {
			for _, item := range cb.CallbackMetadata.Item {
				switch item.Name {
				case "MpesaReceiptNumber":
					if val, ok := item.Value.(string); ok {
						confirmedReceipt = strings.TrimSpace(val)
					}
				case "PhoneNumber":
					tx.PhoneNumber = fmt.Sprintf("%v", item.Value)
				case "Amount":
					// Round half away from zero so 0.29 -> 29 cents (not 28 via
					// float truncation). M-Pesa may return the value as a number
					// or string depending on the callback type.
					if cents, err := parseMoneyCents(item.Value); err == nil && cents > 0 {
						confirmedAmount = cents
					}
				}
			}
		}
		if confirmedReceipt == "" || confirmedAmount <= 0 {
			return fmt.Errorf("stk callback missing confirmed receipt or valid amount")
		}
		tx.MpesaReceiptNumber = confirmedReceipt
		tx.Amount = confirmedAmount
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
		return err
	}

	return s.repo.UpdateMpesaReconciliation(tx)
}

// HandleC2BValidation validates incoming C2B payment before acceptance.
func (s *service) HandleC2BValidation(signatureHeader string, req C2BValidationRequest) (C2BValidationResponse, error) {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, nil, signatureHeader) {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: DescInvalidSignature}, ErrInvalidSignature
	}

	amount, err := parseMoneyString(req.TransAmount)
	if err != nil || amount <= 0 {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: "Invalid amount"}, nil
	}

	return C2BValidationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
}

// HandleC2BConfirmation handles confirmed C2B payment with automatic reconciliation.
func (s *service) HandleC2BConfirmation(signatureHeader string, req C2BConfirmationRequest, businessID uuid.UUID) (C2BConfirmationResponse, error) {
	if !ValidateCallbackSignature(s.cfg.MpesaCallbackSecret, nil, signatureHeader) {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: DescInvalidSignature}, ErrInvalidSignature
	}

	transID := strings.TrimSpace(req.TransID)
	if transID == "" {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Missing TransID"}, errors.New("missing trans_id")
	}

	// Idempotency: skip duplicate TransID
	if existing, err := s.repo.GetMpesaTransactionByReceipt(transID); err == nil && existing != nil {
		return C2BConfirmationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
	}

	amountCents, err := parseMoneyString(req.TransAmount)
	if err != nil || amountCents <= 0 {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Invalid amount"}, nil
	}

	phone := NormalizePhone(req.MSISDN)
	billRef := strings.TrimSpace(req.BillRefNumber)
	if billRef == "" {
		billRef = strings.TrimSpace(req.InvoiceNumber)
	}

	// C2B confirmations arrive on the public webhook route with no user JWT, so
	// the business must be resolved from the payment details (or the configured
	// deployment business) rather than from request context.
	if businessID == uuid.Nil {
		businessID = s.resolveC2BBusiness(billRef, phone)
	}
	if businessID == uuid.Nil {
		return C2BConfirmationResponse{ResultCode: 1, ResultDesc: "Could not resolve business"}, errors.New("resolve business for c2b confirmation")
	}

	raw, _ := json.Marshal(req)
	tx := &MpesaTransaction{
		BusinessID:           businessID,
		TransactionType:      TypeC2B,
		MpesaReceiptNumber:   transID,
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
	// reconciliation failed on the first attempt. A duplicate-insert error
	// means another delivery already recorded this receipt, so treat it as an
	// accepted duplicate rather than a persistence failure.
	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		if isMpesaReceiptConflict(err) {
			return C2BConfirmationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
		}
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
			if existing.TransactionType != TypeB2C || existing.Amount != input.Amount || existing.PhoneNumber != phone {
				return nil, ErrIdempotencyKeyConflict
			}
			// Do not return a failed transaction as a successful replay.
			if existing.Status == StatusFailed {
				return nil, ErrIdempotencyKeyConflict
			}
			return existing, nil
		}
	}

	if s.cfg != nil && s.cfg.MpesaBusinessID != "" {
		if boundID, err := uuid.Parse(s.cfg.MpesaBusinessID); err == nil && boundID != businessID {
			return nil, ErrBusinessNotBound
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

	// Persist pending transaction before provider request to reserve idempotency key
	if err := s.repo.CreateMpesaTransaction(tx); err != nil {
		if input.IdempotencyKey != "" {
			if existing, lookupErr := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); lookupErr == nil && existing != nil {
				// The concurrent winner must describe the same request, otherwise
				// a different payment could be silently returned.
				if existing.TransactionType != TypeB2C || existing.Amount != input.Amount || existing.PhoneNumber != phone {
					return nil, ErrIdempotencyKeyConflict
				}
				return existing, nil
			}
		}
		return nil, fmt.Errorf("create pending transaction: %w", err)
	}

	resp, err := s.mpesaClient.InitiateB2CPayout(ctx, darajaReq)
	if err != nil {
		if errors.Is(err, ErrMpesaPreDispatch) {
			// Proven pre-dispatch failure: release the reservation so a retry can
			// dispatch instead of replaying a stuck pending row.
			if delErr := s.repo.DeleteMpesaTransaction(tx.ID); delErr != nil {
				log.Printf("payments: release b2c reservation %s failed: %v", tx.ID, delErr)
			}
			return nil, fmt.Errorf("initiate b2c: %w", err)
		}
		if resp != nil {
			tx.CheckoutRequestID = resp.ConversationID
			tx.ResultDesc = resp.ResponseDescription
			if errors.Is(err, ErrTransactionFailed) || (resp.ResponseCode != "" && resp.ResponseCode != "0") {
				tx.Status = StatusFailed
			} else if errors.Is(err, ErrTransactionAmbiguous) {
				// Non-final: a late result callback (allowed from timed_out) can
				// still settle the payout without risking a second send.
				tx.Status = StatusTimedOut
			}
			if saveErr := s.repo.UpdateMpesaTransaction(tx); saveErr != nil {
				log.Printf("payments: save b2c failure for %s failed: %v", tx.ID, saveErr)
			}
		}
		return nil, fmt.Errorf("initiate b2c: %w", err)
	}

	tx.CheckoutRequestID = resp.ConversationID
	tx.ResultDesc = resp.ResponseDescription

	if err := s.repo.UpdateMpesaTransaction(tx); err != nil {
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
			ResultCode     *int   `json:"ResultCode"`
			ResultDesc     string `json:"ResultDesc"`
			ConversationID string `json:"ConversationID"`
			TransactionID  string `json:"TransactionID"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode b2c body: %w", err)
	}

	res := payload.Result
	// A missing ResultCode is not success; refuse to complete the payout.
	if res.ResultCode == nil {
		return fmt.Errorf("b2c result missing ResultCode")
	}
	tx, err := s.repo.GetMpesaTransactionByCheckoutRequestID(res.ConversationID)
	if err != nil {
		return fmt.Errorf("transaction not found for conversation %s: %w", res.ConversationID, err)
	}

	if tx.Status.IsTerminal() {
		return nil
	}

	tx.ResultCode = res.ResultCode
	tx.ResultDesc = res.ResultDesc
	tx.RawPayload = string(body)

	terminal := StatusFailed
	if *res.ResultCode == 0 {
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

	// A prior run may already have booked this payout as an expense. Check
	// before settling a supplier so a retry cannot pay the same money twice.
	if s.financeSvc != nil {
		existing, err := s.findMpesaExpense(tx, marker)
		if err != nil {
			return err
		}
		if existing != nil {
			tx.ReconciliationStatus = Reconciled
			tx.ReconciledEntityType = EntityExpense
			tx.ReconciledEntityID = &existing.ID
			return nil
		}
	}

	if tx.SupplierID != nil {
		if s.suppliersSvc == nil {
			return errors.New("supplier service not configured")
		}
		exists, err := s.supplierPaymentExists(*tx.SupplierID, tx.BusinessID, marker)
		if err != nil {
			return err
		}
		if exists {
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

	// Guard: if this M-Pesa receipt has already been applied to any target
	// (invoice, customer credit, or supplier), do not re-apply it. This
	// prevents double-settlement when reconciliation is retried after a save
	// failure leaves the receipt eligible in the review queue.
	allNotes, err := s.repo.ListAllMpesaPaymentNotes(tx.BusinessID)
	if err != nil {
		return fmt.Errorf("list mpesa payment notes: %w", err)
	}
	for _, note := range allNotes {
		if strings.HasPrefix(note, marker) {
			return nil // already settled; leave transaction as-is
		}
	}

	// 1. Try matching Invoice by AccountReference or BillRefNumber
	ref := strings.TrimSpace(tx.BillRefNumber)
	if ref == "" {
		ref = strings.TrimSpace(tx.AccountReference)
	}
	if ref != "" && s.invoicesSvc != nil {
		inv, err := s.repo.FindInvoiceByNumber(tx.BusinessID, ref)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			// A read failure is not a "no match": stop so the receipt stays
			// available for review instead of being credited to another target.
			return fmt.Errorf("find invoice %s: %w", ref, err)
		}
		if err == nil && inv != nil && inv.Status != invoices.StatusPaid {
			exists, err := s.invoicePaymentExists(inv.ID, tx.BusinessID, marker)
			if err != nil {
				return err
			}
			if !exists {
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
		cust, err := s.repo.FindCustomerByPhone(tx.BusinessID, tx.PhoneNumber)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find customer for %s: %w", tx.PhoneNumber, err)
		}
		if err == nil && cust != nil {
			exists, err := s.customerPaymentExists(cust.ID, tx.BusinessID, marker)
			if err != nil {
				return err
			}
			if !exists {
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

// resolveC2BBusiness finds the tenant that owns a public C2B confirmation. It
// prefers an invoice matched by BillRefNumber, then a customer matched by phone,
// then the deployment-wide MPESA_BUSINESS_ID. A nil result means the payment
// cannot be attributed and must be rejected.
func (s *service) resolveC2BBusiness(billRef, phone string) uuid.UUID {
	if billRef != "" {
		if inv, err := s.repo.FindInvoiceByNumber(uuid.Nil, billRef); err == nil && inv != nil {
			return inv.BusinessID
		}
	}
	if phone != "" {
		if cust, err := s.repo.FindCustomerByPhone(uuid.Nil, phone); err == nil && cust != nil {
			return cust.BusinessID
		}
	}
	if s.cfg != nil && s.cfg.MpesaBusinessID != "" {
		if id, err := uuid.Parse(s.cfg.MpesaBusinessID); err == nil {
			return id
		}
	}
	return uuid.Nil
}

func (s *service) invoicePaymentExists(invoiceID, businessID uuid.UUID, marker string) (bool, error) {
	if s.invoicesSvc == nil {
		return false, errors.New("invoice service not configured")
	}
	payments, err := s.invoicesSvc.ListPayments(invoiceID, businessID)
	if err != nil {
		return false, fmt.Errorf("list invoice payments: %w", err)
	}
	for _, payment := range payments {
		if strings.HasPrefix(payment.Note, marker) {
			return true, nil
		}
	}
	return false, nil
}

func (s *service) customerPaymentExists(customerID, businessID uuid.UUID, marker string) (bool, error) {
	if s.customersSvc == nil {
		return false, errors.New("customer service not configured")
	}
	payments, err := s.customersSvc.ListPayments(customerID, businessID)
	if err != nil {
		return false, fmt.Errorf("list customer payments: %w", err)
	}
	for _, payment := range payments {
		if strings.HasPrefix(payment.Note, marker) {
			return true, nil
		}
	}
	return false, nil
}

func (s *service) supplierPaymentExists(supplierID, businessID uuid.UUID, marker string) (bool, error) {
	if s.suppliersSvc == nil {
		return false, errors.New("supplier service not configured")
	}
	payments, err := s.suppliersSvc.ListPayments(supplierID, businessID)
	if err != nil {
		return false, fmt.Errorf("list supplier payments: %w", err)
	}
	for _, payment := range payments {
		if strings.HasPrefix(payment.Note, marker) {
			return true, nil
		}
	}
	return false, nil
}

// receiptAlreadyApplied reports whether an M-Pesa receipt number has already
// been recorded against any invoice, customer, or supplier payment. The
// callback path can leave a receipt unmatched when its reconciliation save
// fails, so the review path must refuse a second settlement of the same
// receipt rather than checking only the selected target.
func (s *service) receiptAlreadyApplied(receipt string, businessID uuid.UUID) (bool, error) {
	receipt = strings.TrimSpace(receipt)
	if receipt == "" {
		return false, nil
	}
	notes, err := s.repo.ListAllMpesaPaymentNotes(businessID)
	if err != nil {
		// Fail closed: if we cannot verify, stop reconciliation rather than risk
		// applying the receipt a second time.
		return false, fmt.Errorf("list mpesa payment notes: %w", err)
	}
	marker := "M-Pesa " + receipt
	for _, note := range notes {
		if strings.HasPrefix(note, marker) {
			return true, nil
		}
	}
	return false, nil
}

// findBankExpense reuses an operating expense already posted for this bank
// transaction so a retry after a failed reconciliation link does not duplicate
// it. It matches on the exact description and amount within a bounded window.
func (s *service) findBankExpense(businessID uuid.UUID, ctx *reviewReconcileContext, description string) (*finance.Expense, error) {
	if s.financeSvc == nil || ctx.bankTx == nil {
		return nil, nil
	}
	base := ctx.bankTx.TransactedAt
	if base.IsZero() {
		base = time.Now()
	}
	expenses, err := s.financeSvc.ListExpenses(businessID, base.Add(-24*time.Hour), base.Add(24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("list expenses for dedupe: %w", err)
	}
	for i := range expenses {
		if expenses[i].Amount == ctx.amount && expenses[i].Description == description {
			return &expenses[i], nil
		}
	}
	return nil, nil
}

// findMpesaExpense checks a bounded window around the transaction for an
// expense already created for this payout.
func (s *service) findMpesaExpense(tx *MpesaTransaction, marker string) (*finance.Expense, error) {
	if s.financeSvc == nil {
		return nil, nil
	}
	from := tx.CreatedAt.Add(-24 * time.Hour)
	to := time.Now().Add(24 * time.Hour)
	if tx.CreatedAt.IsZero() {
		from = time.Now().Add(-365 * 24 * time.Hour)
	}
	expenses, err := s.financeSvc.ListExpenses(tx.BusinessID, from, to)
	if err != nil {
		// Fail closed: without the lookup we cannot prove the payout was not
		// already booked, so surface the error instead of creating a duplicate.
		return nil, fmt.Errorf("list expenses for dedupe: %w", err)
	}
	for i := range expenses {
		if strings.HasPrefix(expenses[i].Description, marker) {
			return &expenses[i], nil
		}
	}
	return nil, nil
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

	var txType BankTransactionType
	normalizedType := strings.ToLower(strings.TrimSpace(input.Type))
	switch normalizedType {
	case "inflow", "deposit":
		txType = BankInflow
	case "outflow", "withdrawal":
		txType = BankOutflow
	default:
		return nil, errors.New("invalid transaction type: must be inflow or outflow")
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
			if inv, err := s.repo.FindInvoiceByNumber(tx.BusinessID, token); err == nil && inv != nil && inv.Status != invoices.StatusPaid && tx.Amount == inv.TotalAmount-inv.AmountPaid {
				if s.invoicesSvc != nil {
					note := fmt.Sprintf("Bank transfer %s (%s)", tx.Reference, tx.BankName)
					_, err := s.invoicesSvc.RecordPayment(inv.ID, tx.BusinessID, invoices.RecordPaymentInput{
						Amount: tx.Amount,
						Note:   note,
					})
					if err == nil {
						tx.ReconciliationStatus = Reconciled
						tx.ReconciledEntityType = EntityInvoice
						tx.ReconciledEntityID = &inv.ID
						return
					}
					// Recover payment if already committed before read failure
					if exists, chkErr := s.invoicePaymentExists(inv.ID, tx.BusinessID, note); chkErr == nil && exists {
						tx.ReconciliationStatus = Reconciled
						tx.ReconciledEntityType = EntityInvoice
						tx.ReconciledEntityID = &inv.ID
						return
					}
					// If the error was uncertain (not an explicit business logic rejection), stop matching to prevent double payment
					if !errors.Is(err, invoices.ErrInvoiceAlreadyPaid) && !errors.Is(err, invoices.ErrPaymentExceedsBalance) && !errors.Is(err, invoices.ErrInvalidPayment) && !errors.Is(err, invoices.ErrInvoiceNotFound) {
						tx.ReconciliationStatus = Unmatched
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
	if amountIdx == -1 && inflowIdx == -1 && outflowIdx == -1 {
		return nil, fmt.Errorf("%w: missing amount column", ErrInvalidStatementFile)
	}

	res := &StatementImportResult{TotalRows: len(records) - 1}

	for _, row := range records[1:] {
		if len(row) <= refIdx {
			res.InvalidRows++
			continue
		}
		ref := strings.TrimSpace(row[refIdx])
		if ref == "" {
			res.InvalidRows++
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
			rawDate := strings.TrimSpace(row[dateIdx])
			if rawDate != "" {
				parsedDate, perr := time.Parse("2006-01-02", rawDate)
				if perr != nil {
					parsedDate, perr = time.Parse(time.RFC3339, rawDate)
				}
				if perr != nil {
					// Do not silently substitute the import date for a bad value.
					res.InvalidRows++
					continue
				}
				transDate = parsedDate
			}
		}
		if transDate.IsZero() {
			transDate = time.Now()
		}

		var inflowCents, outflowCents int64
		if inflowIdx >= 0 && inflowIdx < len(row) && strings.TrimSpace(row[inflowIdx]) != "" {
			if v, err := parseMoneyString(row[inflowIdx]); err == nil && v > 0 {
				inflowCents = v
			}
		}
		if outflowIdx >= 0 && outflowIdx < len(row) && strings.TrimSpace(row[outflowIdx]) != "" {
			if v, err := parseMoneyString(row[outflowIdx]); err == nil && v > 0 {
				outflowCents = v
			}
		}

		var amountCents int64
		txType := BankInflow
		switch {
		case inflowCents > 0 && outflowCents > 0:
			// Both a credit and a debit is not one unambiguous movement.
			res.InvalidRows++
			continue
		case inflowCents > 0:
			amountCents = inflowCents
			txType = BankInflow
		case outflowCents > 0:
			amountCents = outflowCents
			txType = BankOutflow
		default:
			if amountIdx >= 0 && amountIdx < len(row) {
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
		}

		if amountCents <= 0 {
			res.InvalidRows++
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
	if s == "" {
		return 0, errors.New("empty amount")
	}

	negative := false
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		negative = s[0] == '-'
		s = s[1:]
	}

	var intPart, fracPart string
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart = s[:dot]
		fracPart = s[dot+1:]
	} else {
		intPart = s
	}

	if intPart == "" && fracPart == "" {
		return 0, errors.New("empty amount")
	}

	// Validate integer part contains only ASCII digits
	for _, ch := range intPart {
		if ch < '0' || ch > '9' {
			return 0, errors.New("invalid characters in integer part of amount")
		}
	}
	// Validate fractional part contains only ASCII digits
	for _, ch := range fracPart {
		if ch < '0' || ch > '9' {
			return 0, errors.New("invalid characters in fractional part of amount")
		}
	}

	intCents := int64(0)
	if intPart != "" {
		n, err := strconv.ParseInt(intPart, 10, 64)
		if err != nil {
			return 0, err
		}
		// Guard the scaling before it silently wraps. int64 max is
		// 9223372036854775807, so any integer part above
		// 92233720368547758 would overflow when multiplied by 100.
		if n > math.MaxInt64/100 {
			return 0, errors.New("amount out of range")
		}
		intCents = n * 100
	}

	// Parse the fractional part without floating point so 0.29 -> 29 cents
	// (not 28 via int64(0.29*100)). Keep up to 3 digits: 2 for cents, 1 for
	// round-half-away-from-zero.
	fracCents := int64(0)
	if fracPart != "" {
		digits := fracPart
		if len(digits) > 3 {
			digits = digits[:3]
		}
		for len(digits) < 3 {
			digits += "0"
		}
		centsDigits := digits[:2]
		roundDigit := digits[2]
		cents, err := strconv.ParseInt(centsDigits, 10, 64)
		if err != nil {
			return 0, err
		}
		if roundDigit >= '5' {
			cents++
		}
		fracCents = cents
	}

	// Check combined magnitude before addition and applying sign
	if intCents > math.MaxInt64-fracCents {
		return 0, errors.New("amount out of range")
	}

	total := intCents + fracCents
	if negative {
		total = -total
	}
	return total, nil
}

// parseMoneyCents converts an M-Pesa callback metadata value (float64, int,
// string, or json.Number) to cents without floating-point truncation.
func parseMoneyCents(v any) (int64, error) {
	switch val := v.(type) {
	case string:
		return parseMoneyString(val)
	case float64:
		if val < 0 {
			return -float64ToCents(-val), nil
		}
		return float64ToCents(val), nil
	case int:
		return int64(val) * 100, nil
	case int64:
		return val * 100, nil
	case json.Number:
		return parseMoneyString(val.String())
	default:
		return 0, fmt.Errorf("unsupported amount type %T", v)
	}
}

// float64ToCents converts a float64 amount to cents rounding half away from
// zero, avoiding the float truncation bug where int64(0.29*100) == 28.
func float64ToCents(f float64) int64 {
	return int64(math.Floor(f*100 + 0.5))
}

// ListBankTransactions returns paginated bank transactions.
func (s *service) ListBankTransactions(businessID uuid.UUID, txType string, status string, page, limit int) ([]BankTransaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if (page - 1) > (math.MaxInt-1)/limit {
		return nil, 0, errors.New("page out of bounds")
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
	if (page - 1) > (math.MaxInt-1)/limit {
		return nil, 0, errors.New("page out of bounds")
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
	if (page - 1) > (math.MaxInt-1)/limit {
		return nil, 0, errors.New("page out of bounds")
	}
	offset := (page - 1) * limit
	// Sweep interrupted reconciliation claims so stranded rows become reviewable.
	_ = s.repo.ReleaseStaleReconciliationClaims(time.Now().Add(-reconciliationClaimTTL))
	return s.repo.ListReviewQueue(businessID, source, limit, offset)
}

// reconciliationClaimTTL is how long a Claiming row may sit before it is
// considered abandoned and released back to Unmatched.
const reconciliationClaimTTL = 5 * time.Minute

// ReconcileReviewItem manually settles an unmatched transaction. Inflows may
// only settle invoices or customer credit; outflows may only record an expense.
func (s *service) ReconcileReviewItem(businessID uuid.UUID, itemID uuid.UUID, input ReconcileInput) (err error) {
	ctx, err := s.loadReviewReconcileContext(businessID, itemID, input)
	if err != nil {
		return err
	}

	if err := s.ensureReviewDirectionAllowed(ctx.direction, input.TargetType); err != nil {
		return err
	}

	// Release any claim abandoned by an interrupted prior attempt so it becomes
	// reviewable again before we try to take a new claim.
	_ = s.repo.ReleaseStaleReconciliationClaims(time.Now().Add(-reconciliationClaimTTL))

	// Claim the reconciliation slot before applying any financial effect. The
	// claim is a compare-and-set on reconciliation_status (Unmatched -> Claiming),
	// so only one concurrent request can settle the row. The claim is keyed on
	// the row id, not the receipt, so a blank-receipt payout is still serialized.
	var release func()
	if ctx.mpesaTx != nil {
		claimed, claimErr := s.repo.ClaimMpesaReconciliation(ctx.mpesaTx, Unmatched, Claiming)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			return ErrAlreadyReconciled
		}
		release = func() {
			_, _ = s.repo.ClaimMpesaReconciliation(ctx.mpesaTx, Claiming, Unmatched)
		}
	} else if ctx.bankTx != nil {
		claimed, claimErr := s.repo.ClaimBankReconciliation(ctx.bankTx, Unmatched, Claiming)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			return ErrAlreadyReconciled
		}
		release = func() {
			_, _ = s.repo.ClaimBankReconciliation(ctx.bankTx, Claiming, Unmatched)
		}
	}

	committed := false
	if release != nil {
		// Runs on error, panic, or a bare return. A successful settlement sets
		// committed, and the transient state is replaced with Reconciled by the
		// finalize step. A release that fails is swept by the stale-claim cleanup.
		defer func() {
			if !committed {
				release()
			}
		}()
	}

	// One M-Pesa receipt must settle exactly one target. Check the receipt against
	// every payment table, not just the selected target. This needs a receipt, so
	// it stays guarded even though the claim above does not.
	if ctx.mpesaTx != nil && ctx.mpesaTx.MpesaReceiptNumber != "" {
		already, chkErr := s.receiptAlreadyApplied(ctx.mpesaTx.MpesaReceiptNumber, businessID)
		if chkErr != nil {
			return chkErr
		}
		if already {
			return ErrAlreadyReconciled
		}
	}

	note := input.Note
	if note == "" {
		note = fmt.Sprintf("Reconciled from %s (%s)", input.Source, ctx.refString)
	}

	switch input.TargetType {
	case "invoice":
		err = s.reconcileReviewInvoice(businessID, ctx, input, note)
	case "customer_credit":
		err = s.reconcileReviewCustomerCredit(businessID, ctx, input, note)
	case "expense":
		err = s.reconcileReviewExpense(businessID, ctx, input, note)
	case "supplier_payment":
		err = s.reconcileReviewSupplierPayment(businessID, ctx, input, note)
	case "dismiss":
		err = s.reconcileReviewDismiss(ctx)
	default:
		err = ErrInvalidTarget
	}
	if err == nil {
		committed = true
	}
	return err
}

type reviewReconcileContext struct {
	amount    int64
	refString string
	direction string
	mpesaTx   *MpesaTransaction
	bankTx    *BankTransaction
}

func (s *service) loadReviewReconcileContext(businessID uuid.UUID, itemID uuid.UUID, input ReconcileInput) (*reviewReconcileContext, error) {
	ctx := &reviewReconcileContext{}

	switch input.Source {
	case "mpesa":
		tx, err := s.repo.GetMpesaTransactionByID(itemID, businessID)
		if err != nil {
			return nil, ErrTransactionNotFound
		}
		if tx.ReconciliationStatus == Reconciled {
			return nil, ErrAlreadyReconciled
		}
		if tx.Status != StatusCompleted {
			return nil, ErrTransactionNotCompleted
		}
		ctx.mpesaTx = tx
		ctx.amount = tx.Amount
		ctx.refString = tx.MpesaReceiptNumber
		ctx.direction = string(BankInflow)
		if tx.TransactionType == TypeB2C {
			ctx.direction = string(BankOutflow)
		}
	case "bank":
		tx, err := s.repo.GetBankTransactionByID(itemID, businessID)
		if err != nil {
			return nil, ErrTransactionNotFound
		}
		if tx.ReconciliationStatus == Reconciled {
			return nil, ErrAlreadyReconciled
		}
		ctx.bankTx = tx
		ctx.amount = tx.Amount
		ctx.refString = tx.Reference
		ctx.direction = string(tx.Type)
	default:
		return nil, ErrInvalidTarget
	}

	return ctx, nil
}

func (s *service) ensureReviewDirectionAllowed(direction, targetType string) error {
	switch targetType {
	case "invoice", "customer_credit":
		if direction == string(BankOutflow) {
			return ErrTargetDirectionMismatch
		}
	case "expense", "supplier_payment":
		if direction == string(BankInflow) {
			return ErrTargetDirectionMismatch
		}
	}
	return nil
}

func (s *service) finalizeMpesaReconciliation(tx *MpesaTransaction, entityType ReconciledEntityType, entityID uuid.UUID) error {
	entityIDCopy := entityID
	tx.ReconciliationStatus = Reconciled
	tx.ReconciledEntityType = entityType
	tx.ReconciledEntityID = &entityIDCopy
	return s.repo.UpdateMpesaReconciliation(tx)
}

func (s *service) finalizeBankReconciliation(tx *BankTransaction, entityType ReconciledEntityType, entityID uuid.UUID) error {
	entityIDCopy := entityID
	tx.ReconciliationStatus = Reconciled
	tx.ReconciledEntityType = entityType
	tx.ReconciledEntityID = &entityIDCopy
	return s.repo.UpdateBankTransaction(tx)
}

func (s *service) reconcileReviewInvoice(businessID uuid.UUID, ctx *reviewReconcileContext, input ReconcileInput, note string) error {
	targetUUID, err := uuid.Parse(input.TargetID)
	if err != nil {
		return fmt.Errorf("invalid invoice id: %w", err)
	}
	if s.invoicesSvc == nil {
		return errors.New("invoice service not configured")
	}

	paymentNote := note
	if ctx.mpesaTx != nil {
		marker := mpesaMarker(ctx.mpesaTx)
		paymentNote = marker + " invoice payment: " + note
		exists, err := s.invoicePaymentExists(targetUUID, businessID, marker)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.invoicesSvc.RecordPayment(targetUUID, businessID, invoices.RecordPaymentInput{
				Amount: ctx.amount,
				Note:   paymentNote,
			}); err != nil {
				return fmt.Errorf("record invoice payment: %w", err)
			}
		}
	} else if _, err := s.invoicesSvc.RecordPayment(targetUUID, businessID, invoices.RecordPaymentInput{
		Amount: ctx.amount,
		Note:   paymentNote,
	}); err != nil {
		return fmt.Errorf("record invoice payment: %w", err)
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntityInvoice, targetUUID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntityInvoice, targetUUID)
	}
	return nil
}

func (s *service) reconcileReviewCustomerCredit(businessID uuid.UUID, ctx *reviewReconcileContext, input ReconcileInput, note string) error {
	targetUUID, err := uuid.Parse(input.TargetID)
	if err != nil {
		return fmt.Errorf("invalid customer id: %w", err)
	}
	if s.customersSvc == nil {
		return errors.New("customer service not configured")
	}

	paymentNote := note
	if ctx.mpesaTx != nil {
		marker := mpesaMarker(ctx.mpesaTx)
		paymentNote = marker + " customer payment: " + note
		exists, err := s.customerPaymentExists(targetUUID, businessID, marker)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.customersSvc.RecordPayment(targetUUID, businessID, ctx.amount, paymentNote); err != nil {
				return fmt.Errorf("record customer payment: %w", err)
			}
		}
	} else if _, err := s.customersSvc.RecordPayment(targetUUID, businessID, ctx.amount, paymentNote); err != nil {
		return fmt.Errorf("record customer payment: %w", err)
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntityCustomerCredit, targetUUID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntityCustomerCredit, targetUUID)
	}
	return nil
}

func (s *service) reconcileReviewExpense(businessID uuid.UUID, ctx *reviewReconcileContext, input ReconcileInput, note string) error {
	if s.financeSvc == nil {
		return errors.New("finance service not configured")
	}

	description := note
	var expenseID uuid.UUID
	if ctx.mpesaTx != nil {
		marker := mpesaMarker(ctx.mpesaTx)
		description = marker + " payout: " + note
		existing, findErr := s.findMpesaExpense(ctx.mpesaTx, marker)
		if findErr != nil {
			return findErr
		}
		if existing != nil {
			expenseID = existing.ID
		} else {
			exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
				BusinessID:  businessID,
				Category:    strings.TrimSpace(input.ExpenseCategory),
				Description: description,
				Amount:      ctx.amount,
				IncurredAt:  time.Now(),
			})
			if err != nil {
				return fmt.Errorf("create expense: %w", err)
			}
			expenseID = exp.ID
		}
	} else {
		// Bank path: reuse an expense posted by an earlier attempt (for example a
		// retry after the reconciliation link failed) and post to the
		// transaction's own date so review does not shift the accounting period.
		incurredAt := time.Now()
		if ctx.bankTx != nil {
			incurredAt = ctx.bankTx.TransactedAt
			existing, findErr := s.findBankExpense(businessID, ctx, description)
			if findErr != nil {
				return findErr
			}
			if existing != nil {
				expenseID = existing.ID
			}
		}
		if expenseID == uuid.Nil {
			exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
				BusinessID:  businessID,
				Category:    strings.TrimSpace(input.ExpenseCategory),
				Description: description,
				Amount:      ctx.amount,
				IncurredAt:  incurredAt,
			})
			if err != nil {
				return fmt.Errorf("create expense: %w", err)
			}
			expenseID = exp.ID
		}
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntityExpense, expenseID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntityExpense, expenseID)
	}
	return nil
}

func (s *service) reconcileReviewSupplierPayment(businessID uuid.UUID, ctx *reviewReconcileContext, input ReconcileInput, note string) error {
	supplierUUID, err := uuid.Parse(input.TargetID)
	if err != nil {
		return fmt.Errorf("invalid supplier id: %w", err)
	}
	if s.suppliersSvc == nil {
		return errors.New("supplier service not configured")
	}

	paymentNote := note
	if ctx.mpesaTx != nil {
		marker := mpesaMarker(ctx.mpesaTx)
		paymentNote = marker + " supplier payment: " + note
		exists, err := s.supplierPaymentExists(supplierUUID, businessID, marker)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.suppliersSvc.RecordPayment(supplierUUID, businessID, ctx.amount, paymentNote); err != nil {
				return fmt.Errorf("record supplier payment: %w", err)
			}
		}
	} else if _, err := s.suppliersSvc.RecordPayment(supplierUUID, businessID, ctx.amount, paymentNote); err != nil {
		return fmt.Errorf("record supplier payment: %w", err)
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntitySupplierPayment, supplierUUID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntitySupplierPayment, supplierUUID)
	}
	return nil
}

func (s *service) reconcileReviewDismiss(ctx *reviewReconcileContext) error {
	if ctx.mpesaTx != nil {
		ctx.mpesaTx.ReconciliationStatus = Manual
		return s.repo.UpdateMpesaReconciliation(ctx.mpesaTx)
	}
	if ctx.bankTx != nil {
		ctx.bankTx.ReconciliationStatus = Manual
		return s.repo.UpdateBankTransaction(ctx.bankTx)
	}
	return nil
}
