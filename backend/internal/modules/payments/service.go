package payments

import (
	"context"
	"crypto/hmac"
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
	ErrPageOutOfBounds         = errors.New("page out of bounds")
	// ErrAmountNotWholeKSh rejects amounts Daraja cannot send exactly: the API
	// takes whole shillings, so a sub-shilling remainder would be silently
	// rounded while the stored amount stayed in cents.
	ErrAmountNotWholeKSh = errors.New("amount must be a whole number of shillings")
	// ErrPayoutExceedsPayable rejects a supplier payout larger than the amount
	// still owed after in-flight payouts, which the payable cannot settle.
	ErrPayoutExceedsPayable = errors.New("payout exceeds the supplier's outstanding balance")
)

const (
	defaultExpenseCategory  = "Operations"
	queueTimeoutDescription = "Queue timeout - awaiting final result"
)

type Service interface {
	InitiateSTKPush(ctx context.Context, businessID uuid.UUID, input STKPushInput) (*MpesaTransaction, error)
	HandleSTKCallback(signatureHeader string, body []byte) error
	HandleC2BValidation(signatureHeader string, body []byte, req C2BValidationRequest) (C2BValidationResponse, error)
	HandleC2BConfirmation(signatureHeader string, body []byte, req C2BConfirmationRequest, businessID uuid.UUID) (C2BConfirmationResponse, error)
	InitiateB2C(ctx context.Context, businessID uuid.UUID, input B2CPayoutInput) (*MpesaTransaction, error)
	HandleB2CResult(signatureHeader string, body []byte) error
	HandleB2CTimeout(signatureHeader string, body []byte) error

	RecordManualBankTransfer(businessID uuid.UUID, input BankManualInput, canPostExpenses bool) (*BankTransaction, error)
	ImportBankStatement(businessID uuid.UUID, bankName string, r io.Reader, canPostExpenses bool) (*StatementImportResult, error)
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
	if err != nil || s.cfg == nil {
		return raw
	}
	token := s.callbackToken()
	if token == "" {
		return raw
	}
	query := parsed.Query()
	query.Set("token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// callbackToken returns the value embedded in provider callback URLs. It is the
// explicit MPESA_CALLBACK_TOKEN when set, otherwise a derived token, and never
// the raw HMAC signing secret.
func (s *service) callbackToken() string {
	if s.cfg == nil {
		return ""
	}
	if token := strings.TrimSpace(s.cfg.MpesaCallbackToken); token != "" {
		return token
	}
	return DeriveCallbackToken(s.cfg.MpesaCallbackSecret)
}

// validCallback accepts either an HMAC/direct-secret signature or the dedicated
// callback token embedded in the provider URL.
func (s *service) validCallback(body []byte, signatureHeader string) bool {
	if s.cfg == nil {
		return false
	}
	sig := strings.TrimSpace(signatureHeader)
	if sig == "" {
		return false
	}
	// Strong path: HMAC over the body (hex or base64).
	if ValidateHMACSignature(s.cfg.MpesaCallbackSecret, body, sig) {
		return true
	}
	// Otherwise accept the dedicated URL token (explicit or derived). The raw
	// signing secret is never accepted as a bearer credential.
	if token := s.callbackToken(); token != "" {
		return hmac.Equal([]byte(sig), []byte(token))
	}
	return false
}

// stkReplayMatches reports whether an existing idempotent STK row describes the
// same request, including the account/invoice being settled.
func stkReplayMatches(existing *MpesaTransaction, input STKPushInput, phone, accountRef string) bool {
	existingRef := existing.AccountReference
	if existingRef == "" {
		existingRef = existing.BillRefNumber
	}
	return existing.TransactionType == TypeSTKPush &&
		existing.Amount == input.Amount &&
		existing.PhoneNumber == phone &&
		existingRef == accountRef
}

// b2cReplayMatches includes the payout target (supplier or expense category).
func b2cReplayMatches(existing *MpesaTransaction, input B2CPayoutInput, phone string, supplierID *uuid.UUID, expenseCategory string) bool {
	if existing.TransactionType != TypeB2C ||
		existing.Amount != input.Amount ||
		existing.PhoneNumber != phone ||
		existing.ExpenseCategory != expenseCategory {
		return false
	}
	return sameUUIDPtr(existing.SupplierID, supplierID)
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// replayResolvedMpesa applies one replay policy: a failed row conflicts, an
// acknowledged dispatch replays without another provider call, and an
// unacknowledged dispatch stays ambiguous even if it carries an id.
func replayResolvedMpesa(existing *MpesaTransaction) (*MpesaTransaction, error) {
	if existing.Status == StatusFailed {
		return nil, ErrIdempotencyKeyConflict
	}
	if existing.Status != StatusCompleted && !existing.DispatchAcknowledged {
		return nil, fmt.Errorf("%w: previous attempt is unresolved", ErrTransactionAmbiguous)
	}
	return existing, nil
}

// preDispatchRetryDesc marks a reservation whose provider call never left the
// building and whose cleanup also failed. A later replay may clear it and
// dispatch once more.
const preDispatchRetryDesc = "pre-dispatch failure - safe to retry"

func retryablePreDispatch(tx *MpesaTransaction) bool {
	return tx.Status == StatusFailed &&
		!tx.DispatchAcknowledged &&
		tx.CheckoutRequestID == "" &&
		tx.ResultDesc == preDispatchRetryDesc
}

// InitiateSTKPush starts an M-Pesa STK push prompt with idempotency protection.
func (s *service) InitiateSTKPush(ctx context.Context, businessID uuid.UUID, input STKPushInput) (*MpesaTransaction, error) {
	if input.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if input.Amount%100 != 0 {
		return nil, ErrAmountNotWholeKSh
	}
	phone := NormalizePhone(input.PhoneNumber)
	if phone == "" {
		return nil, ErrMissingPhone
	}

	accountRef := strings.TrimSpace(input.AccountReference)
	if accountRef == "" {
		accountRef = "BusinessOS"
	}

	// Idempotency: return an existing transaction only when the full request
	// intent matches, including the invoice/account being settled. A reused key
	// for a different invoice must not silently return the first payment.
	if input.IdempotencyKey != "" {
		if existing, err := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); err == nil && existing != nil {
			if !stkReplayMatches(existing, input, phone, accountRef) {
				return nil, ErrIdempotencyKeyConflict
			}
			if retryablePreDispatch(existing) {
				// The provider call never left the building and the original
				// cleanup also failed. Clear it so this retry can dispatch.
				if delErr := s.repo.DeleteMpesaTransaction(existing.ID); delErr != nil {
					return nil, fmt.Errorf("%w: could not clear the previous pre-dispatch reservation", ErrTransactionAmbiguous)
				}
			} else {
				return replayResolvedMpesa(existing)
			}
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

	// Daraja takes whole KSh units; the amount was validated as a positive
	// whole-KSh value before reserving.
	kshAmount := input.Amount / 100

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
				if !stkReplayMatches(existing, input, phone, accountRef) {
					return nil, ErrIdempotencyKeyConflict
				}
				return replayResolvedMpesa(existing)
			}
		}
		return nil, fmt.Errorf("create pending transaction: %w", err)
	}

	// Make call to Daraja gateway
	resp, err := s.mpesaClient.InitiateSTKPush(ctx, darajaReq)
	if err != nil {
		if errors.Is(err, ErrMpesaPreDispatch) {
			// Proven pre-dispatch failure: release the reservation so a retry can
			// dispatch instead of replaying a stuck pending row. If cleanup also
			// fails, persist a terminal retry marker so one replay can still
			// dispatch instead of blocking forever on an unacknowledged row.
			if delErr := s.repo.DeleteMpesaTransaction(tx.ID); delErr != nil {
				log.Printf("payments: release stk reservation %s failed: %v", tx.ID, delErr)
				tx.Status = StatusFailed
				tx.ResultDesc = preDispatchRetryDesc
				if saveErr := s.repo.UpdateMpesaTransaction(tx); saveErr != nil {
					log.Printf("payments: mark stk reservation %s retryable failed: %v", tx.ID, saveErr)
				}
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
		} else {
			// No response at all (e.g. a timeout after the request may have
			// reached Daraja). Keep a recoverable non-final state so the row is
			// surfaced for manual resolution; never report it as a success.
			tx.Status = StatusTimedOut
			if saveErr := s.repo.UpdateMpesaTransaction(tx); saveErr != nil {
				log.Printf("payments: save ambiguous stk for %s failed: %v", tx.ID, saveErr)
			}
		}
		return nil, fmt.Errorf("initiate stk push: %w", err)
	}

	tx.MerchantRequestID = resp.MerchantRequestID
	tx.CheckoutRequestID = resp.CheckoutRequestID
	tx.ResultDesc = resp.ResponseDescription
	// Only a clean provider success proves the dispatch was acknowledged.
	tx.DispatchAcknowledged = true

	if err := s.repo.UpdateMpesaTransaction(tx); err != nil {
		return nil, fmt.Errorf("save transaction: %w", err)
	}

	return tx, nil
}

// HandleSTKCallback processes M-Pesa STK push callback with signature and idempotency checks.
func (s *service) HandleSTKCallback(signatureHeader string, body []byte) error {
	if !s.validCallback(body, signatureHeader) {
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
					tx.PhoneNumber = formatPhoneValue(item.Value)
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
	// closes the double-apply window reported on the original ordering. A timed
	// out push is not final: the prompt may have reached the customer, so a late
	// success may still resolve it.
	claimed, err := s.repo.ClaimMpesaTransactionStatus(tx, terminal, StatusPending, StatusTimedOut)
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
	wasDismissed := tx.ReconciliationStatus == Manual
	if err := s.autoReconcileMpesa(tx); err != nil {
		log.Printf("payments: stk reconciliation for transaction %s failed: %v", tx.ID, err)
		if wasDismissed {
			// The receipt confirmed but could not be posted; reopen the dismissed
			// row so an operator can recover it instead of losing the link.
			if reopenErr := s.repo.ReopenMpesaReconciliation(tx); reopenErr != nil {
				log.Printf("payments: reopen dismissed stk %s failed: %v", tx.ID, reopenErr)
			}
		}
		return err
	}

	// Adopt a Manual row dismissed before this late success, then persist an
	// open row through the guarded update.
	if err := s.repo.AdoptMpesaReconciliation(tx); err != nil {
		return err
	}
	return s.repo.UpdateMpesaReconciliation(tx)
}

// HandleC2BValidation validates incoming C2B payment before acceptance.
func (s *service) HandleC2BValidation(signatureHeader string, body []byte, req C2BValidationRequest) (C2BValidationResponse, error) {
	if !s.validCallback(body, signatureHeader) {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: DescInvalidSignature}, ErrInvalidSignature
	}

	amount, err := parseMoneyString(req.TransAmount)
	if err != nil || amount <= 0 {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: "Invalid amount"}, nil
	}

	// Reject a payment we could not attribute at confirmation time, otherwise
	// Daraja captures it but the confirmation silently drops it. The configured
	// deployment business acts as the fallback.
	billRef := strings.TrimSpace(req.BillRefNumber)
	if billRef == "" {
		billRef = strings.TrimSpace(req.InvoiceNumber)
	}
	if s.resolveC2BBusiness(billRef, NormalizePhone(req.MSISDN)) == uuid.Nil {
		return C2BValidationResponse{ResultCode: 1, ResultDesc: "Unknown account"}, nil
	}

	return C2BValidationResponse{ResultCode: 0, ResultDesc: DescAccepted}, nil
}

// HandleC2BConfirmation handles confirmed C2B payment with automatic reconciliation.
func (s *service) HandleC2BConfirmation(signatureHeader string, body []byte, req C2BConfirmationRequest, businessID uuid.UUID) (C2BConfirmationResponse, error) {
	if !s.validCallback(body, signatureHeader) {
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
	if input.Amount%100 != 0 {
		return nil, ErrAmountNotWholeKSh
	}
	phone := NormalizePhone(input.PhoneNumber)
	if phone == "" {
		return nil, ErrMissingPhone
	}

	// A payout to a supplier must remember its target so the result callback
	// settles the payable instead of recording a generic expense.
	var supplierID *uuid.UUID
	var supplier *suppliers.Supplier
	if raw := strings.TrimSpace(input.SupplierID); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, ErrInvalidTarget
		}
		if s.suppliersSvc == nil {
			return nil, errors.New("supplier service not configured")
		}
		found, err := s.suppliersSvc.Get(parsed, businessID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, suppliers.ErrSupplierNotFound) {
				return nil, ErrSupplierNotFound
			}
			return nil, err
		}
		supplier = found
		supplierID = &parsed
	}

	expenseCategory := strings.TrimSpace(input.ExpenseCat)
	if expenseCategory == "" {
		expenseCategory = defaultExpenseCategory
	}

	if s.cfg != nil && s.cfg.MpesaBusinessID != "" {
		if boundID, err := uuid.Parse(s.cfg.MpesaBusinessID); err == nil && boundID != businessID {
			return nil, ErrBusinessNotBound
		}
	}

	// Idempotency: the settlement target (supplier or expense category) is part
	// of the request intent, so a reused key cannot replay a different payout.
	if input.IdempotencyKey != "" {
		if existing, err := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); err == nil && existing != nil {
			if !b2cReplayMatches(existing, input, phone, supplierID, expenseCategory) {
				return nil, ErrIdempotencyKeyConflict
			}
			if retryablePreDispatch(existing) {
				if delErr := s.repo.DeleteMpesaTransaction(existing.ID); delErr != nil {
					return nil, fmt.Errorf("%w: could not clear the previous pre-dispatch reservation", ErrTransactionAmbiguous)
				}
			} else {
				return replayResolvedMpesa(existing)
			}
		}
	}

	// A supplier payout must not exceed what is still payable after the payouts
	// already in flight. This check runs only when a new reservation is created:
	// an idempotent replay must not count its own pending row against the
	// balance. The repository repeats the check under a supplier row lock so
	// concurrent requests cannot both spend the same balance.
	if supplierID != nil {
		pending, err := s.repo.PendingSupplierPayoutTotal(businessID, *supplierID)
		if err != nil {
			return nil, fmt.Errorf("read pending supplier payouts: %w", err)
		}
		if input.Amount > supplier.OutstandingBalance-pending {
			return nil, ErrPayoutExceedsPayable
		}
	}

	shortCode := s.cfg.MpesaB2CShortCode
	if shortCode == "" {
		shortCode = s.cfg.MpesaShortCode
	}
	kshAmount := input.Amount / 100

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

	// Persist pending transaction before provider request to reserve idempotency
	// key. A supplier payout is inserted through the atomic payable check.
	var createErr error
	if supplierID != nil {
		createErr = s.repo.CreateSupplierPayout(tx)
	} else {
		createErr = s.repo.CreateMpesaTransaction(tx)
	}
	if createErr != nil {
		if errors.Is(createErr, ErrPayoutExceedsPayable) {
			return nil, ErrPayoutExceedsPayable
		}
		if input.IdempotencyKey != "" {
			if existing, lookupErr := s.repo.GetMpesaTransactionByIdempotency(businessID, input.IdempotencyKey); lookupErr == nil && existing != nil {
				// The concurrent winner must describe the same request, otherwise
				// a different payment could be silently returned.
				if !b2cReplayMatches(existing, input, phone, supplierID, expenseCategory) {
					return nil, ErrIdempotencyKeyConflict
				}
				return replayResolvedMpesa(existing)
			}
		}
		return nil, fmt.Errorf("create pending transaction: %w", createErr)
	}

	resp, err := s.mpesaClient.InitiateB2CPayout(ctx, darajaReq)
	if err != nil {
		if errors.Is(err, ErrMpesaPreDispatch) {
			// Proven pre-dispatch failure: release the reservation so a retry can
			// dispatch instead of replaying a stuck pending row. If cleanup also
			// fails, persist a terminal retry marker so one replay can dispatch.
			if delErr := s.repo.DeleteMpesaTransaction(tx.ID); delErr != nil {
				log.Printf("payments: release b2c reservation %s failed: %v", tx.ID, delErr)
				tx.Status = StatusFailed
				tx.ResultDesc = preDispatchRetryDesc
				if saveErr := s.repo.UpdateMpesaTransaction(tx); saveErr != nil {
					log.Printf("payments: mark b2c reservation %s retryable failed: %v", tx.ID, saveErr)
				}
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
	// Only a clean provider success proves the dispatch was acknowledged.
	tx.DispatchAcknowledged = true

	if err := s.repo.UpdateMpesaTransaction(tx); err != nil {
		return nil, fmt.Errorf("save transaction: %w", err)
	}

	return tx, nil
}

// HandleB2CResult processes the B2C callback from Safaricom.
func (s *service) HandleB2CResult(signatureHeader string, body []byte) error {
	if !s.validCallback(body, signatureHeader) {
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
	wasDismissed := tx.ReconciliationStatus == Manual
	if err := s.applyB2CPayout(tx); err != nil {
		log.Printf("payments: b2c reconciliation for transaction %s failed: %v", tx.ID, err)
		if wasDismissed {
			// The payout confirmed but could not be posted; reopen the dismissed
			// row so an operator can recover the settlement instead of losing it.
			if reopenErr := s.repo.ReopenMpesaReconciliation(tx); reopenErr != nil {
				log.Printf("payments: reopen dismissed b2c %s failed: %v", tx.ID, reopenErr)
			}
		}
		return nil
	}

	// Adopt a Manual row that was dismissed before this late result (the update
	// is scoped to Manual, so it is a no-op otherwise); then persist an open
	// row through the normal guarded update.
	if err := s.repo.AdoptMpesaReconciliation(tx); err != nil {
		return err
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
	if !s.validCallback(body, signatureHeader) {
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
	allNotes, err := s.repo.ListMpesaPaymentNotesByPrefix(tx.BusinessID, marker)
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

// bankMarker is the stable prefix that identifies payments applied for one bank
// transaction, mirroring mpesaMarker so manual review retries dedupe instead of
// recording a second payment.
func bankMarker(tx *BankTransaction) string {
	return "Bank tx " + tx.ID.String()
}

// formatPhoneValue renders a Daraja callback phone value without the scientific
// notation that "%v" produces for a JSON float64, then normalizes it.
func formatPhoneValue(v any) string {
	switch n := v.(type) {
	case string:
		return NormalizePhone(n)
	case float64:
		return NormalizePhone(strconv.FormatInt(int64(n), 10))
	case int64:
		return NormalizePhone(strconv.FormatInt(n, 10))
	case int:
		return NormalizePhone(strconv.Itoa(n))
	case json.Number:
		return NormalizePhone(n.String())
	default:
		return NormalizePhone(fmt.Sprintf("%v", v))
	}
}

// resolveC2BBusiness finds the tenant that owns a public C2B confirmation. A
// configured MPESA_BUSINESS_ID owns the deployment-wide Daraja account, so it
// wins over any reference that happens to match another tenant's invoice.
// Without a binding, fall back to an invoice matched by BillRefNumber and then
// to a customer matched by phone. A nil result means the payment cannot be
// attributed and must be rejected.
func (s *service) resolveC2BBusiness(billRef, phone string) uuid.UUID {
	if s.cfg != nil && s.cfg.MpesaBusinessID != "" {
		if id, err := uuid.Parse(s.cfg.MpesaBusinessID); err == nil {
			return id
		}
	}
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
	marker := "M-Pesa " + receipt
	notes, err := s.repo.ListMpesaPaymentNotesByPrefix(businessID, marker)
	if err != nil {
		// Fail closed: if we cannot verify, stop reconciliation rather than risk
		// applying the receipt a second time.
		return false, fmt.Errorf("list mpesa payment notes: %w", err)
	}
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
	marker := bankMarker(ctx.bankTx)
	for i := range expenses {
		if expenses[i].Amount == ctx.amount && strings.HasPrefix(expenses[i].Description, marker) {
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
func (s *service) RecordManualBankTransfer(businessID uuid.UUID, input BankManualInput, canPostExpenses bool) (*BankTransaction, error) {
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

	// Persist as Unmatched first so reconciliation effects are only applied to a
	// durable row; a failed insert must not leave money applied with no record.
	if err := s.repo.CreateBankTransaction(tx); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrDuplicateReference
		}
		return nil, err
	}

	// Automatic reconciliation matching on Reference and Description. The
	// posting may have committed even if linking the reconciliation failed, so
	// report the durable row as unmatched for the review queue rather than
	// claiming a reconciliation that was not stored.
	if err := s.autoReconcileBank(tx, canPostExpenses); err != nil {
		log.Printf("payments: automatic bank reconciliation for %s failed: %v", tx.ID, err)
		tx.ReconciliationStatus = Unmatched
		tx.ReconciledEntityType = ""
		tx.ReconciledEntityID = nil
	}

	return tx, nil
}

// autoReconcileBank claims the bank row with the same exclusive claim used by
// manual review, then attempts automatic matching. If review already owns the
// claim, automatic posting is skipped; if automatic posting loses its claim
// before finalizing, it cannot overwrite the replacement attempt.
func (s *service) autoReconcileBank(tx *BankTransaction, canPostExpenses bool) error {
	claimID := uuid.New()
	claimed, err := s.repo.ClaimBankReconciliation(tx, Unmatched, Claiming, claimID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	tx.ReconciliationClaimID = &claimID
	committed := false
	defer func() {
		if !committed {
			_, _ = s.repo.ReleaseBankReconciliationClaim(tx, claimID)
		}
	}()

	s.matchBankTransaction(tx, canPostExpenses)
	if tx.ReconciliationStatus != Reconciled {
		return nil
	}
	ok, err := s.repo.FinalizeBankReconciliation(tx, claimID)
	if err != nil {
		tx.ReconciliationStatus = Unmatched
		return err
	}
	if !ok {
		tx.ReconciliationStatus = Unmatched
		return ErrAlreadyReconciled
	}
	tx.ReconciliationClaimID = nil
	committed = true
	return nil
}

// matchBankTransaction matches bank inflows against invoices and outflows
// against expenses. It only mutates the in-memory row; the caller commits it.
func (s *service) matchBankTransaction(tx *BankTransaction, canPostExpenses bool) {
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
					note := bankMarker(tx) + " invoice payment: " + fmt.Sprintf("Bank transfer %s (%s)", tx.Reference, tx.BankName)
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
	} else if tx.Type == BankOutflow && s.financeSvc != nil && canPostExpenses {
		// Outflow matched as expense. The caller must hold FinanceManage: the
		// expense boundary is enforced here because automatic matching skips
		// the review-queue permission check.
		exp, err := s.financeSvc.CreateExpense(finance.CreateExpenseInput{
			BusinessID:  tx.BusinessID,
			Category:    "Operations",
			Description: bankMarker(tx) + " Bank outflow " + tx.Reference + ": " + tx.Description,
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
func (s *service) ImportBankStatement(businessID uuid.UUID, bankName string, r io.Reader, canPostExpenses bool) (*StatementImportResult, error) {
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

		inflowCents, ok := parseStatementAmount(row, inflowIdx)
		if !ok {
			res.InvalidRows++
			continue
		}
		outflowCents, ok := parseStatementAmount(row, outflowIdx)
		if !ok {
			res.InvalidRows++
			continue
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

		// Persist the row first so a reconciliation effect is never applied
		// without a durable transaction to dedupe against on a retry.
		if err := s.repo.CreateBankTransaction(tx); err != nil {
			continue
		}
		if err := s.autoReconcileBank(tx, canPostExpenses); err != nil {
			log.Printf("payments: automatic statement reconciliation for %s failed: %v", tx.ID, err)
			tx.ReconciliationStatus = Unmatched
			tx.ReconciledEntityType = ""
			tx.ReconciledEntityID = nil
		}
		res.Imported++
		if tx.ReconciliationStatus == Reconciled {
			res.Reconciled++
		} else {
			res.Unmatched++
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

// parseStatementAmount parses an optional credit or debit column. A blank cell
// is zero so the other side can still be used; a nonblank value that is
// unparseable or negative is invalid. Treating a broken value as zero would
// otherwise import the opposite column and post the wrong movement.
func parseStatementAmount(row []string, idx int) (int64, bool) {
	if idx < 0 || idx >= len(row) {
		return 0, true
	}
	raw := strings.TrimSpace(row[idx])
	if raw == "" {
		return 0, true
	}
	v, err := parseMoneyString(raw)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
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
		return nil, 0, ErrPageOutOfBounds
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
		return nil, 0, ErrPageOutOfBounds
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
		return nil, 0, ErrPageOutOfBounds
	}
	offset := (page - 1) * limit
	// Sweep interrupted reconciliation claims so stranded rows become reviewable.
	// Surface a cleanup failure instead of returning a queue that may silently
	// omit an expired claim.
	if err := s.repo.ReleaseStaleReconciliationClaims(time.Now().Add(-reconciliationClaimTTL)); err != nil {
		return nil, 0, fmt.Errorf("release stale reconciliation claims: %w", err)
	}
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
	// reviewable again before we try to take a new claim. A cleanup failure must
	// not be mistaken for ErrAlreadyReconciled below.
	if err := s.repo.ReleaseStaleReconciliationClaims(time.Now().Add(-reconciliationClaimTTL)); err != nil {
		return fmt.Errorf("release stale reconciliation claims: %w", err)
	}

	// Claim the reconciliation slot before applying any financial effect. The
	// claim is a compare-and-set on reconciliation_status (Unmatched -> Claiming)
	// with a unique owner id, so only one concurrent attempt can settle the row
	// and a stale attempt cannot release or finalize a replacement's claim.
	claimID := uuid.New()
	ctx.claimID = claimID
	var release func()
	if ctx.mpesaTx != nil {
		claimed, claimErr := s.repo.ClaimMpesaReconciliation(ctx.mpesaTx, Unmatched, Claiming, claimID)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			return ErrAlreadyReconciled
		}
		release = func() {
			_, _ = s.repo.ReleaseMpesaReconciliationClaim(ctx.mpesaTx, claimID)
		}
	} else if ctx.bankTx != nil {
		claimed, claimErr := s.repo.ClaimBankReconciliation(ctx.bankTx, Unmatched, Claiming, claimID)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			return ErrAlreadyReconciled
		}
		release = func() {
			_, _ = s.repo.ReleaseBankReconciliationClaim(ctx.bankTx, claimID)
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

	// A posting may have committed before its reconciliation link update failed.
	// Recover it by the stable source marker before considering the newly
	// selected target, otherwise the same money could be applied twice. This
	// applies to M-Pesa receipts as well as bank transactions, including a
	// dismissal: the row is linked to the posting that actually exists instead
	// of being stranded as unmatched.
	if ctx.mpesaTx != nil {
		entityType, entityID, found, findErr := s.repo.FindReconciliationApplication(businessID, mpesaMarker(ctx.mpesaTx))
		if findErr != nil {
			return findErr
		}
		if found {
			ctx.mpesaTx.ReconciliationStatus = Reconciled
			ctx.mpesaTx.ReconciledEntityType = entityType
			ctx.mpesaTx.ReconciledEntityID = &entityID
			if finalizeErr := s.finalizeMpesaReconciliation(ctx.mpesaTx, entityType, entityID, ctx.claimID); finalizeErr != nil {
				return finalizeErr
			}
			committed = true
			return nil
		}
	}
	if ctx.bankTx != nil {
		entityType, entityID, found, findErr := s.repo.FindReconciliationApplication(businessID, bankMarker(ctx.bankTx))
		if findErr != nil {
			return findErr
		}
		if found {
			ctx.bankTx.ReconciliationStatus = Reconciled
			ctx.bankTx.ReconciledEntityType = entityType
			ctx.bankTx.ReconciledEntityID = &entityID
			if finalizeErr := s.finalizeBankReconciliation(ctx.bankTx, entityType, entityID, ctx.claimID); finalizeErr != nil {
				return finalizeErr
			}
			committed = true
			return nil
		}
	}

	// A timed-out payout is ambiguous: the money may still move. If no posting
	// exists yet, never post it until an authoritative result completes it.
	// Dismissal, or linking the committed posting recovered above, is allowed.
	if ctx.mpesaTx != nil && ctx.mpesaTx.Status == StatusTimedOut && input.TargetType != "dismiss" {
		return ErrTransactionNotCompleted
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

	// Revalidate ownership immediately before the ledger write. If this attempt's
	// claim expired and a replacement took over, it must not post.
	if ctx.claimID != uuid.Nil {
		var owns bool
		var ownErr error
		if ctx.mpesaTx != nil {
			owns, ownErr = s.repo.OwnsMpesaReconciliationClaim(ctx.mpesaTx, ctx.claimID)
		} else if ctx.bankTx != nil {
			owns, ownErr = s.repo.OwnsBankReconciliationClaim(ctx.bankTx, ctx.claimID)
		}
		if ownErr != nil {
			return ownErr
		}
		if !owns {
			return ErrAlreadyReconciled
		}
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
	claimID   uuid.UUID
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
		if tx.Status != StatusCompleted && tx.Status != StatusTimedOut {
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

func (s *service) finalizeMpesaReconciliation(tx *MpesaTransaction, entityType ReconciledEntityType, entityID, claimID uuid.UUID) error {
	entityIDCopy := entityID
	tx.ReconciliationStatus = Reconciled
	tx.ReconciledEntityType = entityType
	tx.ReconciledEntityID = &entityIDCopy
	ok, err := s.repo.FinalizeMpesaReconciliation(tx, claimID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyReconciled
	}
	return nil
}

func (s *service) finalizeBankReconciliation(tx *BankTransaction, entityType ReconciledEntityType, entityID, claimID uuid.UUID) error {
	entityIDCopy := entityID
	tx.ReconciliationStatus = Reconciled
	tx.ReconciledEntityType = entityType
	tx.ReconciledEntityID = &entityIDCopy
	ok, err := s.repo.FinalizeBankReconciliation(tx, claimID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyReconciled
	}
	return nil
}

func (s *service) reconcileReviewInvoice(businessID uuid.UUID, ctx *reviewReconcileContext, input ReconcileInput, note string) error {
	targetUUID, err := uuid.Parse(input.TargetID)
	if err != nil {
		return fmt.Errorf("invalid invoice id: %w", err)
	}
	if s.invoicesSvc == nil {
		return errors.New("invoice service not configured")
	}

	marker := ""
	if ctx.mpesaTx != nil {
		marker = mpesaMarker(ctx.mpesaTx)
	} else if ctx.bankTx != nil {
		marker = bankMarker(ctx.bankTx)
	}
	paymentNote := note
	exists := false
	if marker != "" {
		paymentNote = marker + " invoice payment: " + note
		found, err := s.invoicePaymentExists(targetUUID, businessID, marker)
		if err != nil {
			return err
		}
		exists = found
	}
	if !exists {
		if _, err := s.invoicesSvc.RecordPayment(targetUUID, businessID, invoices.RecordPaymentInput{
			Amount: ctx.amount,
			Note:   paymentNote,
		}); err != nil {
			return fmt.Errorf("record invoice payment: %w", err)
		}
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntityInvoice, targetUUID, ctx.claimID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntityInvoice, targetUUID, ctx.claimID)
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

	marker := ""
	if ctx.mpesaTx != nil {
		marker = mpesaMarker(ctx.mpesaTx)
	} else if ctx.bankTx != nil {
		marker = bankMarker(ctx.bankTx)
	}
	paymentNote := note
	exists := false
	if marker != "" {
		paymentNote = marker + " customer payment: " + note
		found, err := s.customerPaymentExists(targetUUID, businessID, marker)
		if err != nil {
			return err
		}
		exists = found
	}
	if !exists {
		if _, err := s.customersSvc.RecordPayment(targetUUID, businessID, ctx.amount, paymentNote); err != nil {
			return fmt.Errorf("record customer payment: %w", err)
		}
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntityCustomerCredit, targetUUID, ctx.claimID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntityCustomerCredit, targetUUID, ctx.claimID)
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
				Description: bankMarker(ctx.bankTx) + " " + description,
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
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntityExpense, expenseID, ctx.claimID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntityExpense, expenseID, ctx.claimID)
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

	marker := ""
	if ctx.mpesaTx != nil {
		marker = mpesaMarker(ctx.mpesaTx)
	} else if ctx.bankTx != nil {
		marker = bankMarker(ctx.bankTx)
	}
	paymentNote := note
	exists := false
	if marker != "" {
		paymentNote = marker + " supplier payment: " + note
		found, err := s.supplierPaymentExists(supplierUUID, businessID, marker)
		if err != nil {
			return err
		}
		exists = found
	}
	if !exists {
		if _, err := s.suppliersSvc.RecordPayment(supplierUUID, businessID, ctx.amount, paymentNote); err != nil {
			return fmt.Errorf("record supplier payment: %w", err)
		}
	}

	if ctx.mpesaTx != nil {
		return s.finalizeMpesaReconciliation(ctx.mpesaTx, EntitySupplierPayment, supplierUUID, ctx.claimID)
	}
	if ctx.bankTx != nil {
		return s.finalizeBankReconciliation(ctx.bankTx, EntitySupplierPayment, supplierUUID, ctx.claimID)
	}
	return nil
}

func (s *service) reconcileReviewDismiss(ctx *reviewReconcileContext) error {
	if ctx.mpesaTx != nil {
		// Only commit Manual when the transaction status still matches the one
		// observed at load time. A payout that completed while this dismissal was
		// in flight changes the status, so this fails and the deferred release
		// returns the row to the queue for recovery instead of hiding it.
		ok, err := s.repo.FinalizeMpesaDismissal(ctx.mpesaTx, ctx.claimID, ctx.mpesaTx.Status)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAlreadyReconciled
		}
		ctx.mpesaTx.ReconciliationStatus = Manual
		ctx.mpesaTx.ReconciledEntityType = ""
		ctx.mpesaTx.ReconciledEntityID = nil
		return nil
	}
	if ctx.bankTx != nil {
		ctx.bankTx.ReconciliationStatus = Manual
		ctx.bankTx.ReconciledEntityType = ""
		ctx.bankTx.ReconciledEntityID = nil
		ok, err := s.repo.FinalizeBankReconciliation(ctx.bankTx, ctx.claimID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAlreadyReconciled
		}
		return nil
	}
	return nil
}
