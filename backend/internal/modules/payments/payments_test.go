package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PhoneNumberTestConst is a test phone number used in test fixtures.
const PhoneNumberTestConst = "0712345678"

// sourceMpesaTestConst is the M-Pesa review-queue source used in fixtures.
const sourceMpesaTestConst = "mpesa"

// Review target names used in fixtures.
const (
	targetInvoiceTestConst = "invoice"
	targetDismissTestConst = "dismiss"
)

// passkeyTestConst is a placeholder M-Pesa passkey used in test configurations.
const passkeyTestConst = "passkey"

type fakeApplication struct {
	marker     string
	entityType ReconciledEntityType
	entityID   uuid.UUID
}

// In-memory fake repository for payments testing
type fakePaymentRepository struct {
	mpesaTxs          map[string]*MpesaTransaction
	bankTxs           map[string]*BankTransaction
	invoices          map[string]*invoices.Invoice
	custs             map[string]*customers.Customer
	paymentNotes      []string
	application       *fakeApplication
	claimErr          error
	deleteErr         error
	releaseErr        error
	reconciliationErr error
}

func newFakePaymentRepository() *fakePaymentRepository {
	return &fakePaymentRepository{
		mpesaTxs:     make(map[string]*MpesaTransaction),
		bankTxs:      make(map[string]*BankTransaction),
		invoices:     make(map[string]*invoices.Invoice),
		custs:        make(map[string]*customers.Customer),
		paymentNotes: make([]string, 0),
	}
}

func (f *fakePaymentRepository) CreateMpesaTransaction(tx *MpesaTransaction) error {
	if tx.ID == uuid.Nil {
		tx.ID = uuid.New()
	}
	f.mpesaTxs[tx.ID.String()] = tx
	return nil
}

func (f *fakePaymentRepository) UpdateMpesaTransaction(tx *MpesaTransaction) error {
	f.mpesaTxs[tx.ID.String()] = tx
	return nil
}

func (f *fakePaymentRepository) DeleteMpesaTransaction(id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.mpesaTxs, id.String())
	return nil
}

func (f *fakePaymentRepository) ReleaseStaleReconciliationClaims(before time.Time) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	for _, tx := range f.mpesaTxs {
		if tx.ReconciliationStatus == Claiming && tx.UpdatedAt.Before(before) {
			tx.ReconciliationStatus = Unmatched
			tx.ReconciliationClaimID = nil
		}
	}
	for _, tx := range f.bankTxs {
		if tx.ReconciliationStatus == Claiming && tx.UpdatedAt.Before(before) {
			tx.ReconciliationStatus = Unmatched
			tx.ReconciliationClaimID = nil
		}
	}
	return nil
}

func (f *fakePaymentRepository) GetMpesaTransactionByID(id, businessID uuid.UUID) (*MpesaTransaction, error) {
	tx, ok := f.mpesaTxs[id.String()]
	if !ok || tx.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return tx, nil
}

func (f *fakePaymentRepository) GetMpesaTransactionByIdempotency(businessID uuid.UUID, key string) (*MpesaTransaction, error) {
	for _, tx := range f.mpesaTxs {
		if tx.BusinessID == businessID && tx.IdempotencyKey == key {
			return tx, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakePaymentRepository) GetMpesaTransactionByCheckoutRequestID(checkoutReqID string) (*MpesaTransaction, error) {
	for _, tx := range f.mpesaTxs {
		if tx.CheckoutRequestID == checkoutReqID {
			// Return a detached copy so callback mutations only reach the stored
			// row through an explicit repository write.
			detached := *tx
			return &detached, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakePaymentRepository) GetMpesaTransactionByReceipt(receipt string) (*MpesaTransaction, error) {
	for _, tx := range f.mpesaTxs {
		if tx.MpesaReceiptNumber == receipt {
			return tx, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakePaymentRepository) ClaimMpesaTransactionStatus(tx *MpesaTransaction, terminal TransactionStatus, allowedFrom ...TransactionStatus) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if len(allowedFrom) == 0 {
		allowedFrom = []TransactionStatus{StatusPending}
	}
	allowed := false
	for _, status := range allowedFrom {
		if stored.Status == status {
			allowed = true
			break
		}
	}
	if !allowed {
		return false, nil
	}
	stored.Status = terminal
	stored.ResultCode = tx.ResultCode
	stored.ResultDesc = tx.ResultDesc
	stored.RawPayload = tx.RawPayload
	stored.MpesaReceiptNumber = tx.MpesaReceiptNumber
	stored.PhoneNumber = tx.PhoneNumber
	stored.Amount = tx.Amount
	return true, nil
}

func (f *fakePaymentRepository) ClaimMpesaReconciliation(tx *MpesaTransaction, from, to ReconciliationStatus, claimID uuid.UUID) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationStatus != from {
		return false, nil
	}
	stored.ReconciliationStatus = to
	if to == Claiming {
		stored.ReconciliationClaimID = &claimID
	} else {
		stored.ReconciliationClaimID = nil
	}
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) ReleaseMpesaReconciliationClaim(tx *MpesaTransaction, claimID uuid.UUID) (bool, error) {
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationClaimID == nil || *stored.ReconciliationClaimID != claimID {
		return false, nil
	}
	stored.ReconciliationStatus = Unmatched
	stored.ReconciliationClaimID = nil
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) FinalizeMpesaReconciliation(tx *MpesaTransaction, claimID uuid.UUID) (bool, error) {
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationClaimID == nil || *stored.ReconciliationClaimID != claimID {
		return false, nil
	}
	stored.ReconciliationStatus = tx.ReconciliationStatus
	stored.ReconciledEntityType = tx.ReconciledEntityType
	stored.ReconciledEntityID = tx.ReconciledEntityID
	stored.ReconciliationClaimID = nil
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) OwnsMpesaReconciliationClaim(tx *MpesaTransaction, claimID uuid.UUID) (bool, error) {
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	return stored.ReconciliationClaimID != nil && *stored.ReconciliationClaimID == claimID, nil
}

func (f *fakePaymentRepository) ClaimBankReconciliation(tx *BankTransaction, from, to ReconciliationStatus, claimID uuid.UUID) (bool, error) {
	stored, ok := f.bankTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationStatus != from {
		return false, nil
	}
	stored.ReconciliationStatus = to
	if to == Claiming {
		stored.ReconciliationClaimID = &claimID
	} else {
		stored.ReconciliationClaimID = nil
	}
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) ReleaseBankReconciliationClaim(tx *BankTransaction, claimID uuid.UUID) (bool, error) {
	stored, ok := f.bankTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationClaimID == nil || *stored.ReconciliationClaimID != claimID {
		return false, nil
	}
	stored.ReconciliationStatus = Unmatched
	stored.ReconciliationClaimID = nil
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) OwnsBankReconciliationClaim(tx *BankTransaction, claimID uuid.UUID) (bool, error) {
	stored, ok := f.bankTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	return stored.ReconciliationClaimID != nil && *stored.ReconciliationClaimID == claimID, nil
}

func (f *fakePaymentRepository) FinalizeBankReconciliation(tx *BankTransaction, claimID uuid.UUID) (bool, error) {
	stored, ok := f.bankTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationClaimID == nil || *stored.ReconciliationClaimID != claimID {
		return false, nil
	}
	stored.ReconciliationStatus = tx.ReconciliationStatus
	stored.ReconciledEntityType = tx.ReconciledEntityType
	stored.ReconciledEntityID = tx.ReconciledEntityID
	stored.ReconciliationClaimID = nil
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) UpdateMpesaReconciliation(tx *MpesaTransaction) error {
	if f.reconciliationErr != nil {
		return f.reconciliationErr
	}
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	// Mirror the production guard: never overwrite a finalized reconciliation.
	if stored.ReconciliationStatus == Reconciled ||
		stored.ReconciliationStatus == Manual ||
		stored.ReconciliationStatus == Claiming {
		return nil
	}
	stored.ReconciliationStatus = tx.ReconciliationStatus
	stored.ReconciledEntityType = tx.ReconciledEntityType
	stored.ReconciledEntityID = tx.ReconciledEntityID
	return nil
}

func (f *fakePaymentRepository) AdoptMpesaReconciliation(tx *MpesaTransaction) error {
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if stored.ReconciliationStatus != Manual && stored.ReconciliationStatus != Claiming {
		return nil
	}
	stored.ReconciliationStatus = tx.ReconciliationStatus
	stored.ReconciledEntityType = tx.ReconciledEntityType
	stored.ReconciledEntityID = tx.ReconciledEntityID
	stored.ReconciliationClaimID = nil
	return nil
}

func (f *fakePaymentRepository) ReopenMpesaReconciliation(tx *MpesaTransaction) error {
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if stored.ReconciliationStatus != Manual {
		return nil
	}
	stored.ReconciliationStatus = Unmatched
	stored.ReconciledEntityType = ""
	stored.ReconciledEntityID = nil
	stored.ReconciliationClaimID = nil
	return nil
}

func (f *fakePaymentRepository) FinalizeMpesaDismissal(tx *MpesaTransaction, claimID uuid.UUID, expectedStatus TransactionStatus) (bool, error) {
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return false, gorm.ErrRecordNotFound
	}
	if stored.ReconciliationStatus != Claiming ||
		stored.ReconciliationClaimID == nil ||
		*stored.ReconciliationClaimID != claimID ||
		stored.Status != expectedStatus {
		return false, nil
	}
	stored.ReconciliationStatus = Manual
	stored.ReconciledEntityType = ""
	stored.ReconciledEntityID = nil
	stored.ReconciliationClaimID = nil
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (f *fakePaymentRepository) PendingSupplierPayoutTotal(businessID, supplierID uuid.UUID) (int64, error) {
	var total int64
	for _, tx := range f.mpesaTxs {
		if tx.BusinessID == businessID && tx.SupplierID != nil && *tx.SupplierID == supplierID &&
			tx.TransactionType == TypeB2C &&
			(tx.Status == StatusPending || tx.Status == StatusTimedOut) {
			total += tx.Amount
		}
	}
	return total, nil
}

func (f *fakePaymentRepository) CreateSupplierPayout(tx *MpesaTransaction) error {
	return f.CreateMpesaTransaction(tx)
}

func (f *fakePaymentRepository) ListMpesaTransactions(businessID uuid.UUID, limit, offset int) ([]MpesaTransaction, int64, error) {
	list := make([]MpesaTransaction, 0)
	for _, tx := range f.mpesaTxs {
		if tx.BusinessID == businessID {
			list = append(list, *tx)
		}
	}
	return list, int64(len(list)), nil
}

func (f *fakePaymentRepository) CreateBankTransaction(tx *BankTransaction) error {
	for _, b := range f.bankTxs {
		if b.BusinessID == tx.BusinessID && strings.EqualFold(b.Reference, tx.Reference) {
			return ErrDuplicateReference
		}
	}
	if tx.ID == uuid.Nil {
		tx.ID = uuid.New()
	}
	f.bankTxs[tx.ID.String()] = tx
	return nil
}

func (f *fakePaymentRepository) GetBankTransactionByID(id, businessID uuid.UUID) (*BankTransaction, error) {
	tx, ok := f.bankTxs[id.String()]
	if !ok || tx.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return tx, nil
}

func (f *fakePaymentRepository) GetBankTransactionByReference(businessID uuid.UUID, ref string) (*BankTransaction, error) {
	for _, b := range f.bankTxs {
		if b.BusinessID == businessID && strings.EqualFold(b.Reference, ref) {
			return b, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakePaymentRepository) ListBankTransactions(businessID uuid.UUID, txType string, status string, limit, offset int) ([]BankTransaction, int64, error) {
	list := make([]BankTransaction, 0)
	for _, b := range f.bankTxs {
		if b.BusinessID == businessID {
			list = append(list, *b)
		}
	}
	return list, int64(len(list)), nil
}

func (f *fakePaymentRepository) ListReviewQueue(businessID uuid.UUID, source string, limit, offset int) ([]ReviewQueueItem, int64, error) {
	items := make([]ReviewQueueItem, 0)
	if source == "" || source == "all" || source == sourceMpesaTestConst {
		for _, m := range f.mpesaTxs {
			if m.BusinessID == businessID && m.ReconciliationStatus == Unmatched && (m.Status == StatusCompleted || m.Status == StatusPending || m.Status == StatusTimedOut) {
				items = append(items, ReviewQueueItem{
					ID:                m.ID,
					Source:            sourceMpesaTestConst,
					Reference:         m.MpesaReceiptNumber,
					Amount:            m.Amount,
					Type:              "inflow",
					TransactionStatus: string(m.Status),
				})
			}
		}
	}
	if source == "" || source == "all" || source == "bank" {
		for _, b := range f.bankTxs {
			if b.BusinessID == businessID && b.ReconciliationStatus == Unmatched {
				items = append(items, ReviewQueueItem{
					ID:        b.ID,
					Source:    "bank",
					Reference: b.Reference,
					Amount:    b.Amount,
					Type:      string(b.Type),
				})
			}
		}
	}
	return items, int64(len(items)), nil
}

func (f *fakePaymentRepository) FindInvoiceByNumber(businessID uuid.UUID, invoiceNumber string) (*invoices.Invoice, error) {
	var matches []*invoices.Invoice
	for _, inv := range f.invoices {
		if (businessID == uuid.Nil || inv.BusinessID == businessID) && strings.EqualFold(inv.InvoiceNumber, invoiceNumber) {
			matches = append(matches, inv)
		}
	}
	// Mirror production: attribution requires a single unambiguous match.
	if len(matches) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return matches[0], nil
}

func (f *fakePaymentRepository) FindCustomerByPhone(businessID uuid.UUID, phone string) (*customers.Customer, error) {
	norm := NormalizePhone(phone)
	for _, c := range f.custs {
		if (businessID == uuid.Nil || c.BusinessID == businessID) && NormalizePhone(c.Phone) == norm {
			return c, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// FindReconciliationApplication returns a configured fake posting so a retry
// can be shown to recover the original target.
func (f *fakePaymentRepository) FindReconciliationApplication(businessID uuid.UUID, marker string) (ReconciledEntityType, uuid.UUID, bool, error) {
	if f.application != nil && f.application.marker == marker {
		return f.application.entityType, f.application.entityID, true, nil
	}
	return "", uuid.Nil, false, nil
}

// ListMpesaPaymentNotesByPrefix returns the fake's flat notes filtered to the
// prefix, mirroring the SQL prefix filter used in production.
func (f *fakePaymentRepository) ListMpesaPaymentNotesByPrefix(businessID uuid.UUID, prefix string) ([]string, error) {
	matched := make([]string, 0, len(f.paymentNotes))
	for _, note := range f.paymentNotes {
		if strings.HasPrefix(note, prefix) {
			matched = append(matched, note)
		}
	}
	return matched, nil
}

// Mock MpesaClient
type mockMpesaClient struct {
	stkResp    *DarajaSTKPushResponse
	stkErr     error
	b2cResp    *DarajaB2CResponse
	b2cErr     error
	lastReq    DarajaSTKPushRequest
	lastB2CReq DarajaB2CRequest
	stkCalls   int
	b2cCalls   int
}

func (m *mockMpesaClient) InitiateSTKPush(ctx context.Context, req DarajaSTKPushRequest) (*DarajaSTKPushResponse, error) {
	m.stkCalls++
	m.lastReq = req
	return m.stkResp, m.stkErr
}

func (m *mockMpesaClient) InitiateB2CPayout(ctx context.Context, req DarajaB2CRequest) (*DarajaB2CResponse, error) {
	m.b2cCalls++
	m.lastB2CReq = req
	return m.b2cResp, m.b2cErr
}

// Mock invoices service
type mockInvoicesService struct {
	paidInvoiceID uuid.UUID
	paidAmount    int64
	recordErr     error
	payments      []invoices.InvoicePayment
}

func (m *mockInvoicesService) List(businessID uuid.UUID, filter invoices.ListFilter) ([]invoices.Invoice, int64, error) {
	return nil, 0, nil
}
func (m *mockInvoicesService) Get(id, businessID uuid.UUID) (*invoices.Invoice, error) {
	return nil, nil
}
func (m *mockInvoicesService) RecordPayment(id, businessID uuid.UUID, input invoices.RecordPaymentInput) (*invoices.Invoice, error) {
	m.paidInvoiceID = id
	m.paidAmount = input.Amount
	if m.recordErr != nil {
		return nil, m.recordErr
	}
	m.payments = append(m.payments, invoices.InvoicePayment{
		ID:         uuid.New(),
		BusinessID: businessID,
		InvoiceID:  id,
		Amount:     input.Amount,
		Note:       input.Note,
	})
	return &invoices.Invoice{ID: id, BusinessID: businessID, AmountPaid: input.Amount, Status: invoices.StatusPaid}, nil
}

func (m *mockInvoicesService) ListPayments(id, businessID uuid.UUID) ([]invoices.InvoicePayment, error) {
	var result []invoices.InvoicePayment
	for _, payment := range m.payments {
		if payment.InvoiceID == id && payment.BusinessID == businessID {
			result = append(result, payment)
		}
	}
	return result, nil
}

// Mock customers service
type mockCustomersService struct {
	paidCustID uuid.UUID
	paidAmount int64
	payments   []customers.Payment
}

func (m *mockCustomersService) Create(customers.CreateInput) (*customers.Customer, error) {
	return nil, nil
}
func (m *mockCustomersService) Get(uuid.UUID, uuid.UUID) (*customers.Customer, error) {
	return nil, nil
}
func (m *mockCustomersService) List(uuid.UUID) ([]customers.Customer, error) { return nil, nil }
func (m *mockCustomersService) Update(uuid.UUID, uuid.UUID, customers.UpdateInput) (*customers.Customer, error) {
	return nil, nil
}
func (m *mockCustomersService) ListAboveBalance(uuid.UUID, int64) ([]customers.Customer, error) {
	return nil, nil
}
func (m *mockCustomersService) RecordPayment(id, businessID uuid.UUID, amount int64, note string) (*customers.Customer, error) {
	m.paidCustID = id
	m.paidAmount = amount
	m.payments = append(m.payments, customers.Payment{
		ID:         uuid.New(),
		BusinessID: businessID,
		CustomerID: id,
		Amount:     amount,
		Note:       note,
	})
	return &customers.Customer{ID: id, BusinessID: businessID}, nil
}
func (m *mockCustomersService) ListPayments(id, businessID uuid.UUID) ([]customers.Payment, error) {
	var result []customers.Payment
	for _, payment := range m.payments {
		if payment.CustomerID == id && payment.BusinessID == businessID {
			result = append(result, payment)
		}
	}
	return result, nil
}
func (m *mockCustomersService) FindOrCreateByPhone(uuid.UUID, string, string, string) (*customers.Customer, error) {
	return nil, nil
}
func (m *mockCustomersService) ChargeCreditTx(*gorm.DB, uuid.UUID, uuid.UUID, int64) error {
	return nil
}

// Mock suppliers service
type mockSuppliersService struct {
	paidSupplierID uuid.UUID
	paidAmount     int64
	outstanding    int64
	recordErr      error
	payments       []suppliers.Payment
}

func (m *mockSuppliersService) Create(suppliers.CreateInput) (*suppliers.Supplier, error) {
	return nil, nil
}
func (m *mockSuppliersService) Get(id, businessID uuid.UUID) (*suppliers.Supplier, error) {
	if m.paidSupplierID == id {
		return &suppliers.Supplier{ID: id, BusinessID: businessID, OutstandingBalance: m.outstanding}, nil
	}
	return nil, suppliers.ErrSupplierNotFound
}
func (m *mockSuppliersService) List(uuid.UUID, bool) ([]suppliers.Supplier, error) {
	return nil, nil
}
func (m *mockSuppliersService) ListPaginated(uuid.UUID, int, int, string, bool) ([]suppliers.Supplier, int64, error) {
	return nil, 0, nil
}
func (m *mockSuppliersService) GetAging(context.Context, uuid.UUID, time.Time) (*suppliers.AgingReport, error) {
	return nil, nil
}
func (m *mockSuppliersService) Update(uuid.UUID, uuid.UUID, suppliers.UpdateInput) (*suppliers.Supplier, error) {
	return nil, nil
}
func (m *mockSuppliersService) Delete(uuid.UUID, uuid.UUID) error { return nil }
func (m *mockSuppliersService) Restore(uuid.UUID, uuid.UUID) (*suppliers.Supplier, error) {
	return nil, nil
}
func (m *mockSuppliersService) RecordPayment(id, businessID uuid.UUID, amount int64, note string) (*suppliers.Supplier, error) {
	if m.recordErr != nil {
		return nil, m.recordErr
	}
	m.paidSupplierID = id
	m.paidAmount = amount
	m.payments = append(m.payments, suppliers.Payment{
		ID:         uuid.New(),
		BusinessID: businessID,
		SupplierID: id,
		Amount:     amount,
		Note:       note,
	})
	return &suppliers.Supplier{ID: id, BusinessID: businessID}, nil
}
func (m *mockSuppliersService) ListPayments(id, businessID uuid.UUID) ([]suppliers.Payment, error) {
	var result []suppliers.Payment
	for _, payment := range m.payments {
		if payment.SupplierID == id && payment.BusinessID == businessID {
			result = append(result, payment)
		}
	}
	return result, nil
}

// Mock finance service
type mockFinanceService struct {
	createdExpense *finance.Expense
	expenses       []finance.Expense
}

func (m *mockFinanceService) CreateExpense(in finance.CreateExpenseInput) (*finance.Expense, error) {
	exp := &finance.Expense{
		ID:          uuid.New(),
		BusinessID:  in.BusinessID,
		Category:    in.Category,
		Description: in.Description,
		Amount:      in.Amount,
	}
	m.createdExpense = exp
	m.expenses = append(m.expenses, *exp)
	return exp, nil
}
func (m *mockFinanceService) ListExpenses(uuid.UUID, time.Time, time.Time) ([]finance.Expense, error) {
	return m.expenses, nil
}
func (m *mockFinanceService) DeleteExpense(uuid.UUID, uuid.UUID) error { return nil }
func (m *mockFinanceService) Summary(uuid.UUID, time.Time, time.Time) (*finance.Summary, error) {
	return nil, nil
}

func computeHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Test 1: STK push initiation with idempotency key
func TestSTKPushInitiationAndIdempotency(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		stkResp: &DarajaSTKPushResponse{
			MerchantRequestID:   "MR-12345",
			CheckoutRequestID:   "ws_CO_17112026_1",
			ResponseCode:        "0",
			ResponseDescription: "Success. Request accepted for processing",
		},
	}
	//nolint:gosec // G101: Daraja sandbox test credentials — not production secrets
	cfg := &config.Config{
		MpesaShortCode:      "174379",
		MpesaPasskey:        "bfb279f9aa9bdbcf158e97dd71a467cd2e0c893059b10f78e6b72ada1ed2c919",
		MpesaStkCallbackURL: "https://example.com/callback",
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	businessID := uuid.New()
	input := STKPushInput{
		PhoneNumber:      PhoneNumberTestConst,
		Amount:           50000, // KSh 500
		AccountReference: "INV-2026-0001",
		TransactionDesc:  "Payment",
		IdempotencyKey:   "idemp-stk-100",
	}

	tx, err := svc.InitiateSTKPush(context.Background(), businessID, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.CheckoutRequestID != "ws_CO_17112026_1" {
		t.Fatalf("expected CheckoutRequestID ws_CO_17112026_1, got %s", tx.CheckoutRequestID)
	}
	if client.lastReq.Amount != 500 {
		t.Fatalf("expected amount 500 KSh, got %d", client.lastReq.Amount)
	}
	if client.lastReq.PhoneNumber != "254712345678" {
		t.Fatalf("expected normalized phone 254712345678, got %s", client.lastReq.PhoneNumber)
	}

	// Submitting with same idempotency key returns the existing transaction without calling gateway again
	client.stkResp = nil // If called, would fail
	txSecond, err := svc.InitiateSTKPush(context.Background(), businessID, input)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if txSecond.ID != tx.ID {
		t.Fatalf("expected same transaction ID %s, got %s", tx.ID, txSecond.ID)
	}
}

// Test 2: Callback signature validation (valid, invalid, duplicate)
func TestSTKCallbackSignatureAndDuplicateHandling(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{
		MpesaCallbackSecret: "super-secret-callback-key",
	}
	invSvc := &mockInvoicesService{}
	svc := NewService(repo, nil, cfg, invSvc, nil, nil, nil)

	businessID := uuid.New()
	invID := uuid.New()
	repo.invoices["INV-2026-0001"] = &invoices.Invoice{
		ID:            invID,
		BusinessID:    businessID,
		InvoiceNumber: "INV-2026-0001",
		TotalAmount:   50000,
		Status:        invoices.StatusUnpaid,
	}

	// Create pending STK transaction in repo
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		CheckoutRequestID:    "ws_CO_99999",
		AccountReference:     "INV-2026-0001",
		BillRefNumber:        "INV-2026-0001",
		Amount:               50000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	// Callback payload for success
	cbPayload := `{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "MR-1",
				"CheckoutRequestID": "ws_CO_99999",
				"ResultCode": 0,
				"ResultDesc": "The service request is processed successfully.",
				"CallbackMetadata": {
					"Item": [
						{"Name": "Amount", "Value": 500.0},
						{"Name": "MpesaReceiptNumber", "Value": "NLJ7RT61SV"},
						{"Name": "TransactionDate", "Value": 20261117103000},
						{"Name": "PhoneNumber", "Value": 254712345678}
					]
				}
			}
		}
	}`
	bodyBytes := []byte(cbPayload)

	// 1. Invalid signature rejection
	err := svc.HandleSTKCallback("invalid-signature", bodyBytes)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}

	// 2. Valid signature success
	validSig := computeHMAC(cfg.MpesaCallbackSecret, bodyBytes)
	err = svc.HandleSTKCallback(validSig, bodyBytes)
	if err != nil {
		t.Fatalf("expected success with valid signature, got %v", err)
	}

	updatedTx, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if updatedTx.Status != StatusCompleted {
		t.Fatalf("expected status completed, got %s", updatedTx.Status)
	}
	if updatedTx.MpesaReceiptNumber != "NLJ7RT61SV" {
		t.Fatalf("expected receipt NLJ7RT61SV, got %s", updatedTx.MpesaReceiptNumber)
	}
	if updatedTx.ReconciliationStatus != Reconciled || updatedTx.ReconciledEntityType != EntityInvoice {
		t.Fatalf("expected reconciled to invoice, got %s / %s", updatedTx.ReconciliationStatus, updatedTx.ReconciledEntityType)
	}
	if invSvc.paidInvoiceID != invID || invSvc.paidAmount != 50000 {
		t.Fatalf("invoice payment not recorded properly: id=%s amount=%d", invSvc.paidInvoiceID, invSvc.paidAmount)
	}

	// 3. Duplicate callback (Idempotency) -> must not record payment again
	invSvc.paidInvoiceID = uuid.Nil
	invSvc.paidAmount = 0
	err = svc.HandleSTKCallback(validSig, bodyBytes)
	if err != nil {
		t.Fatalf("duplicate callback failed: %v", err)
	}
	if invSvc.paidInvoiceID != uuid.Nil {
		t.Fatal("duplicate callback recorded payment again!")
	}
}

// Test 3: C2B validation and confirmation
func TestC2BValidationAndConfirmation(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{
		MpesaCallbackSecret: "c2b-secret",
	}
	custSvc := &mockCustomersService{}
	svc := NewService(repo, nil, cfg, nil, custSvc, nil, nil)

	businessID := uuid.New()
	custID := uuid.New()
	repo.custs["254700112233"] = &customers.Customer{
		ID:         custID,
		BusinessID: businessID,
		Name:       "Test Customer",
		Phone:      "254700112233",
	}

	// Validation check
	vReq := C2BValidationRequest{
		TransAmount: "250.00",
		MSISDN:      "254700112233",
	}
	vRes, err := svc.HandleC2BValidation(DeriveCallbackToken("c2b-secret"), nil, vReq)
	if err != nil || vRes.ResultCode != 0 {
		t.Fatalf("validation failed: res=%+v err=%v", vRes, err)
	}

	// Confirmation check
	cReq := C2BConfirmationRequest{
		TransID:       "QK12345678",
		TransAmount:   "250.00",
		MSISDN:        "254700112233",
		BillRefNumber: "ACC-CUST",
	}
	cRes, err := svc.HandleC2BConfirmation(DeriveCallbackToken("c2b-secret"), nil, cReq, uuid.Nil)
	if err != nil || cRes.ResultCode != 0 {
		t.Fatalf("confirmation failed: res=%+v err=%v", cRes, err)
	}

	tx, _ := repo.GetMpesaTransactionByReceipt("QK12345678")
	if tx == nil {
		t.Fatal("expected saved C2B transaction")
	}
	if tx.Amount != 25000 {
		t.Fatalf("expected amount 25000 cents, got %d", tx.Amount)
	}
	// A public callback (uuid.Nil) must resolve the owning tenant, not persist a
	// nil business id.
	if tx.BusinessID != businessID {
		t.Fatalf("expected resolved business %s, got %s", businessID, tx.BusinessID)
	}
	if tx.ReconciliationStatus != Reconciled || tx.ReconciledEntityType != EntityCustomerCredit {
		t.Fatalf("expected reconciled to customer credit, got %s / %s", tx.ReconciliationStatus, tx.ReconciledEntityType)
	}
	if custSvc.paidCustID != custID || custSvc.paidAmount != 25000 {
		t.Fatalf("customer payment not credited: id=%s amount=%d", custSvc.paidCustID, custSvc.paidAmount)
	}

	// Duplicate TransID handling: idempotent
	cResDup, err := svc.HandleC2BConfirmation(DeriveCallbackToken("c2b-secret"), nil, cReq, uuid.Nil)
	if err != nil || cResDup.ResultCode != 0 {
		t.Fatalf("duplicate confirmation failed: %v", err)
	}
}

// Test 4: Bank transfer manual entry and duplicate rejection
func TestBankTransferManualEntry(t *testing.T) {
	repo := newFakePaymentRepository()
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, finSvc, nil)

	businessID := uuid.New()
	input := BankManualInput{
		BankName:         "Equity Bank",
		Reference:        "EQ-987654",
		Type:             "outflow",
		Amount:           150000,
		Description:      "Office supplies payment",
		TransactedAt:     time.Now(),
		SettlementIntent: string(EntityExpense),
	}

	tx, err := svc.RecordManualBankTransfer(businessID, input, true)
	if err != nil {
		t.Fatalf("manual entry failed: %v", err)
	}
	if tx.Reference != "EQ-987654" {
		t.Fatalf("expected ref EQ-987654, got %s", tx.Reference)
	}
	if tx.ReconciliationStatus != Reconciled || tx.ReconciledEntityType != EntityExpense {
		t.Fatalf("expected outflow to auto-reconcile as expense, got %s / %s", tx.ReconciliationStatus, tx.ReconciledEntityType)
	}
	if finSvc.createdExpense == nil || finSvc.createdExpense.Amount != 150000 {
		t.Fatalf("expense not created: %+v", finSvc.createdExpense)
	}

	// Duplicate reference error
	_, err = svc.RecordManualBankTransfer(businessID, input, true)
	if !errors.Is(err, ErrDuplicateReference) {
		t.Fatalf("expected ErrDuplicateReference, got %v", err)
	}
}

// Test 5: Bank statement CSV import with duplicate skipping & reconciliation
func TestBankStatementCSVImport(t *testing.T) {
	repo := newFakePaymentRepository()
	invSvc := &mockInvoicesService{}
	svc := NewService(repo, nil, &config.Config{}, invSvc, nil, nil, nil)

	businessID := uuid.New()
	invID := uuid.New()
	repo.invoices["INV-2026-999"] = &invoices.Invoice{
		ID:            invID,
		BusinessID:    businessID,
		InvoiceNumber: "INV-2026-999",
		TotalAmount:   75000,
		Status:        invoices.StatusUnpaid,
	}

	// Pre-seed an existing transaction to test duplicate skipping
	_ = repo.CreateBankTransaction(&BankTransaction{
		BusinessID: businessID,
		Reference:  "REF-PREEXISTING",
		Amount:     1000,
	})

	csvData := `Date,Reference,Description,Credit,Debit
2026-11-15,REF-PREEXISTING,Should be skipped,10.00,0
2026-11-16,REF-INVOICE,INV-2026-999 payment,750.00,0
2026-11-17,REF-UNMATCHED,Walk in deposit,300.00,0
`
	result, err := svc.ImportBankStatement(businessID, "KCB Bank", strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("statement import failed: %v", err)
	}
	if result.TotalRows != 3 {
		t.Fatalf("expected 3 total rows, got %d", result.TotalRows)
	}
	if result.SkippedDuplicates != 1 {
		t.Fatalf("expected 1 duplicate skipped, got %d", result.SkippedDuplicates)
	}
	if result.Imported != 2 {
		t.Fatalf("expected 2 imported, got %d", result.Imported)
	}
	if result.Reconciled != 1 {
		t.Fatalf("expected 1 reconciled (invoice match), got %d", result.Reconciled)
	}
	if result.Unmatched != 1 {
		t.Fatalf("expected 1 unmatched, got %d", result.Unmatched)
	}
}

// Test 6: Review queue listing and manual reconciliation
func TestReviewQueueAndManualReconcile(t *testing.T) {
	repo := newFakePaymentRepository()
	invSvc := &mockInvoicesService{}
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, invSvc, nil, finSvc, nil)

	businessID := uuid.New()
	invID := uuid.New()

	// Seed unmatched M-Pesa transaction
	mpesaTx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeC2B,
		MpesaReceiptNumber:   "REV12345",
		Amount:               8000,
		Status:               StatusCompleted,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(mpesaTx)

	// List review queue
	items, total, err := svc.ListReviewQueue(businessID, "all", 1, 10)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 review item, got %d (err: %v)", total, err)
	}
	if items[0].Reference != "REV12345" {
		t.Fatalf("expected ref REV12345, got %s", items[0].Reference)
	}

	// Manually reconcile to invoice
	recInput := ReconcileInput{
		Source:     sourceMpesaTestConst,
		TargetType: targetInvoiceTestConst,
		TargetID:   invID.String(),
		Note:       "Manual reconciliation",
	}
	err = svc.ReconcileReviewItem(businessID, mpesaTx.ID, recInput)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated, _ := repo.GetMpesaTransactionByID(mpesaTx.ID, businessID)
	if updated.ReconciliationStatus != Reconciled || updated.ReconciledEntityType != EntityInvoice {
		t.Fatalf("expected reconciled, got %s", updated.ReconciliationStatus)
	}
	if invSvc.paidInvoiceID != invID || invSvc.paidAmount != 8000 {
		t.Fatalf("invoice payment not recorded: %d", invSvc.paidAmount)
	}

	// Reconciling again fails
	err = svc.ReconcileReviewItem(businessID, mpesaTx.ID, recInput)
	if !errors.Is(err, ErrAlreadyReconciled) {
		t.Fatalf("expected ErrAlreadyReconciled, got %v", err)
	}
}

// Test 7: a failed status claim must not apply the payment. This is the fix for
// the callback ordering double-apply window.
func TestSTKCallbackDoesNotApplyPaymentWhenClaimFails(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "claim-secret"}
	invSvc := &mockInvoicesService{}
	svc := NewService(repo, nil, cfg, invSvc, nil, nil, nil)

	businessID := uuid.New()
	invID := uuid.New()
	repo.invoices["INV-CLAIM-1"] = &invoices.Invoice{
		ID:            invID,
		BusinessID:    businessID,
		InvoiceNumber: "INV-CLAIM-1",
		TotalAmount:   10000,
		Status:        invoices.StatusUnpaid,
	}
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		CheckoutRequestID:    "ws_CO_CLAIM_FAILS",
		AccountReference:     "INV-CLAIM-1",
		BillRefNumber:        "INV-CLAIM-1",
		Amount:               10000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)
	repo.claimErr = errors.New("database unavailable")

	body := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_CLAIM_FAILS","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"CLAIM123"},{"Name":"Amount","Value":100.0}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err == nil {
		t.Fatal("expected the failed status claim to be reported")
	}
	if invSvc.paidInvoiceID != uuid.Nil {
		t.Fatal("payment must not be applied when the durable status was not saved")
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusPending {
		t.Fatalf("status = %s, want pending", stored.Status)
	}
}

// Test 8: when reconciliation fails after the status is saved, the transaction
// stays completed and unmatched instead of replaying the payment.
func TestSTKCallbackLeavesCompletedUnmatchedWhenPaymentFails(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "reconcile-secret"}
	invSvc := &mockInvoicesService{recordErr: errors.New("invoice already paid")}
	svc := NewService(repo, nil, cfg, invSvc, nil, nil, nil)

	businessID := uuid.New()
	repo.invoices["INV-FAIL-1"] = &invoices.Invoice{
		ID:            uuid.New(),
		BusinessID:    businessID,
		InvoiceNumber: "INV-FAIL-1",
		TotalAmount:   9000,
		Status:        invoices.StatusUnpaid,
	}
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		CheckoutRequestID:    "ws_CO_RECON_FAILS",
		AccountReference:     "INV-FAIL-1",
		BillRefNumber:        "INV-FAIL-1",
		Amount:               9000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_RECON_FAILS","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"FAIL123"},{"Name":"Amount","Value":90.0}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err == nil {
		t.Fatal("expected callback to report the reconciliation failure")
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", stored.Status)
	}
	if stored.ReconciliationStatus != Unmatched {
		t.Fatalf("reconciliation = %s, want unmatched for the review queue", stored.ReconciliationStatus)
	}
}

// Test 9: callback URLs embed a derived token (never the signing secret).
func TestCallbackURLsIncludeSecret(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		stkResp: &DarajaSTKPushResponse{CheckoutRequestID: "ws_CO_URL_1", ResponseCode: "0"},
		b2cResp: &DarajaB2CResponse{ConversationID: "CONV-URL-1", ResponseCode: "0"},
	}
	cfg := &config.Config{
		BackendURL:          "https://api.example.com/",
		MpesaCallbackSecret: "callback-token",
		MpesaShortCode:      "174379",
		MpesaPasskey:        passkeyTestConst,
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	if _, err := svc.InitiateSTKPush(context.Background(), uuid.New(), STKPushInput{
		PhoneNumber: PhoneNumberTestConst,
		Amount:      1000,
	}); err != nil {
		t.Fatalf("InitiateSTKPush failed: %v", err)
	}
	derived := DeriveCallbackToken("callback-token")
	if got, want := client.lastReq.CallBackURL, "https://api.example.com/api/v1/payments/mpesa/callback/stk?token="+derived; got != want {
		t.Fatalf("STK callback URL = %q, want %q", got, want)
	}

	if _, err := svc.InitiateB2C(context.Background(), uuid.New(), B2CPayoutInput{
		PhoneNumber: PhoneNumberTestConst,
		Amount:      1000,
	}); err != nil {
		t.Fatalf("InitiateB2C failed: %v", err)
	}
	if got, want := client.lastB2CReq.ResultURL, "https://api.example.com/api/v1/payments/mpesa/callback/b2c-result?token="+derived; got != want {
		t.Fatalf("B2C result URL = %q, want %q", got, want)
	}
	if got, want := client.lastB2CReq.QueueTimeOutURL, "https://api.example.com/api/v1/payments/mpesa/callback/b2c-timeout?token="+derived; got != want {
		t.Fatalf("B2C timeout URL = %q, want %q", got, want)
	}
}

// Test 10: a supplier-targeted B2C payout settles the payable rather than
// booking a generic expense.
func TestB2CSupplierPayoutSettlesSupplier(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "b2c-secret"}
	supSvc := &mockSuppliersService{}
	finSvc := &mockFinanceService{}
	client := &mockMpesaClient{
		b2cResp: &DarajaB2CResponse{ConversationID: "CONV-SUP-1", ResponseCode: "0"},
	}
	svc := NewService(repo, client, cfg, nil, nil, finSvc, supSvc)

	businessID := uuid.New()
	supplierID := uuid.New()
	supSvc.paidSupplierID = supplierID
	supSvc.outstanding = 25000

	tx, err := svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber: PhoneNumberTestConst,
		Amount:      25000,
		SupplierID:  supplierID.String(),
	})
	if err != nil {
		t.Fatalf("InitiateB2C failed: %v", err)
	}
	if tx.SupplierID == nil || *tx.SupplierID != supplierID {
		t.Fatalf("supplier target not stored: %+v", tx.SupplierID)
	}

	body := []byte(`{"Result":{"ResultCode":0,"ResultDesc":"ok","ConversationID":"CONV-SUP-1","TransactionID":"B2C-RCPT-1"}}`)
	if err := svc.HandleB2CResult(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("HandleB2CResult failed: %v", err)
	}
	if supSvc.paidAmount != 25000 {
		t.Fatalf("supplier payment = %d, want 25000", supSvc.paidAmount)
	}
	if finSvc.createdExpense != nil {
		t.Fatal("supplier payout must not also create an operating expense")
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted || stored.ReconciledEntityType != EntitySupplierPayment {
		t.Fatalf("status/reconciliation = %s/%s, want completed/supplier_payment", stored.Status, stored.ReconciledEntityType)
	}
}

// Test 11: a timeout is not terminal; a later success resolves it exactly once.
func TestB2CTimeoutThenResultCompletes(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "timeout-secret"}
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, cfg, nil, nil, finSvc, nil)

	businessID := uuid.New()
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		CheckoutRequestID:    "CONV-TIMEOUT-1",
		Amount:               30000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
		ExpenseCategory:      "Utilities",
	}
	_ = repo.CreateMpesaTransaction(tx)

	timeoutBody := []byte(`{"Result":{"ConversationID":"CONV-TIMEOUT-1","ResultDesc":"Queue timeout"}}`)
	if err := svc.HandleB2CTimeout(computeHMAC(cfg.MpesaCallbackSecret, timeoutBody), timeoutBody); err != nil {
		t.Fatalf("HandleB2CTimeout failed: %v", err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusTimedOut {
		t.Fatalf("status after timeout = %s, want timed_out", stored.Status)
	}

	resultBody := []byte(`{"Result":{"ResultCode":0,"ResultDesc":"ok","ConversationID":"CONV-TIMEOUT-1","TransactionID":"B2C-LATE-1"}}`)
	if err := svc.HandleB2CResult(computeHMAC(cfg.MpesaCallbackSecret, resultBody), resultBody); err != nil {
		t.Fatalf("HandleB2CResult after timeout failed: %v", err)
	}
	stored, _ = repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status after late result = %s, want completed", stored.Status)
	}
	if finSvc.createdExpense == nil || finSvc.createdExpense.Category != "Utilities" {
		t.Fatalf("expected one Utilities expense, got %+v", finSvc.createdExpense)
	}
	if len(finSvc.expenses) != 1 {
		t.Fatalf("expenses = %d, want 1", len(finSvc.expenses))
	}

	// A second delivery must not create another expense.
	if err := svc.HandleB2CResult(computeHMAC(cfg.MpesaCallbackSecret, resultBody), resultBody); err != nil {
		t.Fatalf("duplicate HandleB2CResult failed: %v", err)
	}
	if len(finSvc.expenses) != 1 {
		t.Fatalf("expenses after duplicate = %d, want 1", len(finSvc.expenses))
	}
}

// Test 12: a timeout must never downgrade an already completed payout.
func TestB2CTimeoutDoesNotOverwriteCompleted(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "timeout-secret"}
	svc := NewService(repo, nil, cfg, nil, nil, &mockFinanceService{}, nil)

	businessID := uuid.New()
	resultCode := 0
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		CheckoutRequestID:    "CONV-DONE-1",
		Amount:               5000,
		Status:               StatusCompleted,
		ResultCode:           &resultCode,
		ResultDesc:           "original result",
		RawPayload:           `{"Result":{"ResultCode":0}}`,
		ReconciliationStatus: Reconciled,
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Result":{"ConversationID":"CONV-DONE-1","ResultDesc":"Queue timeout"}}`)
	if err := svc.HandleB2CTimeout(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("HandleB2CTimeout failed: %v", err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", stored.Status)
	}
	// A rejected timeout must not mutate the stored result metadata.
	if stored.ResultCode == nil || *stored.ResultCode != 0 {
		t.Fatalf("result code = %v, want 0", stored.ResultCode)
	}
	if stored.ResultDesc != "original result" {
		t.Fatalf("result desc = %q, want the original result", stored.ResultDesc)
	}
	if stored.RawPayload != `{"Result":{"ResultCode":0}}` {
		t.Fatalf("raw payload was overwritten: %q", stored.RawPayload)
	}
}

// Daraja can only send whole shillings, so a sub-shilling amount must be
// rejected before a reservation is created or the provider is called.
func TestInitiateRejectsSubShillingAmounts(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		stkResp: &DarajaSTKPushResponse{CheckoutRequestID: "ws_CO_WHOLE", ResponseCode: "0"},
		b2cResp: &DarajaB2CResponse{ConversationID: "CONV-WHOLE", ResponseCode: "0"},
	}
	svc := NewService(repo, client, &config.Config{MpesaShortCode: "174379", MpesaPasskey: passkeyTestConst}, nil, nil, nil, nil)
	businessID := uuid.New()

	if _, err := svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber: PhoneNumberTestConst, Amount: 1050,
	}); !errors.Is(err, ErrAmountNotWholeKSh) {
		t.Fatalf("stk error = %v, want ErrAmountNotWholeKSh", err)
	}
	if _, err := svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber: PhoneNumberTestConst, Amount: 50,
	}); !errors.Is(err, ErrAmountNotWholeKSh) {
		t.Fatalf("b2c error = %v, want ErrAmountNotWholeKSh", err)
	}
	if client.stkCalls != 0 || client.b2cCalls != 0 {
		t.Fatal("invalid amounts must not reach the gateway")
	}
	if len(repo.mpesaTxs) != 0 {
		t.Fatalf("invalid amounts must not reserve a transaction, got %d", len(repo.mpesaTxs))
	}

	// A whole-shilling amount still sends the expected KSh value.
	if _, err := svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber: PhoneNumberTestConst, Amount: 1000,
	}); err != nil {
		t.Fatalf("whole-shilling stk failed: %v", err)
	}
	if client.lastReq.Amount != 10 {
		t.Fatalf("daraja amount = %d, want 10", client.lastReq.Amount)
	}
}

// A late result for a payout the operator dismissed must adopt the settlement
// instead of leaving a Manual row with an unlinked posting.
func TestB2CLateResultAdoptsDismissedPayout(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, cfg, nil, nil, finSvc, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		CheckoutRequestID:    "CONV-DISMISS-1",
		Amount:               30000,
		Status:               StatusTimedOut,
		ReconciliationStatus: Manual,
		ExpenseCategory:      "Utilities",
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Result":{"ResultCode":0,"ResultDesc":"ok","ConversationID":"CONV-DISMISS-1","TransactionID":"B2C-DISMISS-1"}}`)
	if err := svc.HandleB2CResult(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("late result failed: %v", err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", stored.Status)
	}
	if stored.ReconciliationStatus != Reconciled || stored.ReconciledEntityType != EntityExpense {
		t.Fatalf("dismissed payout was not adopted: %s / %s", stored.ReconciliationStatus, stored.ReconciledEntityType)
	}
	if len(finSvc.expenses) != 1 {
		t.Fatalf("expenses = %d, want 1", len(finSvc.expenses))
	}

	// A duplicate result must not post again or change the adopted link.
	if err := svc.HandleB2CResult(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("duplicate result failed: %v", err)
	}
	if len(finSvc.expenses) != 1 {
		t.Fatalf("expenses after duplicate = %d, want 1", len(finSvc.expenses))
	}
}

// Automatic bank outflow matching may only post an expense for callers with
// the finance capability; other callers still record the row for review.
func TestBankOutflowExpenseRequiresFinancePermission(t *testing.T) {
	repo := newFakePaymentRepository()
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, finSvc, nil)
	businessID := uuid.New()

	tx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference:        "OUT-NO-PERM",
		Type:             "withdrawal",
		Amount:           2500,
		Description:      "Supplier payment",
		SettlementIntent: string(EntityExpense),
	}, false)
	if err != nil {
		t.Fatalf("recording an outflow without finance permission failed: %v", err)
	}
	if tx.ReconciliationStatus != Unmatched {
		t.Fatalf("status = %s, want unmatched", tx.ReconciliationStatus)
	}
	if len(finSvc.expenses) != 0 {
		t.Fatal("an outflow without finance permission must not post an expense")
	}

	tx, err = svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference:        "OUT-PERM",
		Type:             "withdrawal",
		Amount:           2500,
		Description:      "Supplier payment",
		SettlementIntent: string(EntityExpense),
	}, true)
	if err != nil {
		t.Fatalf("recording an outflow with finance permission failed: %v", err)
	}
	if tx.ReconciliationStatus != Reconciled || tx.ReconciledEntityType != EntityExpense {
		t.Fatalf("status = %s / %s, want reconciled expense", tx.ReconciliationStatus, tx.ReconciledEntityType)
	}
	if len(finSvc.expenses) != 1 {
		t.Fatalf("expenses = %d, want 1", len(finSvc.expenses))
	}
}

// Test 13: reconciliation targets are direction-aware.
func TestReconcileRejectsDirectionMismatch(t *testing.T) {
	repo := newFakePaymentRepository()
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, finSvc, nil)

	businessID := uuid.New()
	outflow := &BankTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		Reference:            "OUT-1",
		Type:                 BankOutflow,
		Amount:               12000,
		ReconciliationStatus: Unmatched,
		TransactedAt:         time.Now(),
	}
	_ = repo.CreateBankTransaction(outflow)
	inflow := &BankTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		Reference:            "IN-1",
		Type:                 BankInflow,
		Amount:               12000,
		ReconciliationStatus: Unmatched,
		TransactedAt:         time.Now(),
	}
	_ = repo.CreateBankTransaction(inflow)

	if err := svc.ReconcileReviewItem(businessID, outflow.ID, ReconcileInput{
		Source: "bank", TargetType: targetInvoiceTestConst, TargetID: uuid.New().String(),
	}); !errors.Is(err, ErrTargetDirectionMismatch) {
		t.Fatalf("outflow -> invoice error = %v, want ErrTargetDirectionMismatch", err)
	}
	if err := svc.ReconcileReviewItem(businessID, inflow.ID, ReconcileInput{
		Source: "bank", TargetType: string(EntityExpense),
	}); !errors.Is(err, ErrTargetDirectionMismatch) {
		t.Fatalf("inflow -> expense error = %v, want ErrTargetDirectionMismatch", err)
	}

	if err := svc.ReconcileReviewItem(businessID, outflow.ID, ReconcileInput{
		Source: "bank", TargetType: string(EntityExpense), ExpenseCategory: "Transport",
	}); err != nil {
		t.Fatalf("outflow -> expense should succeed: %v", err)
	}
	stored, _ := repo.GetBankTransactionByID(outflow.ID, businessID)
	if stored.ReconciliationStatus != Reconciled || stored.ReconciledEntityType != EntityExpense {
		t.Fatalf("outflow reconciliation = %s/%s, want reconciled/expense", stored.ReconciliationStatus, stored.ReconciledEntityType)
	}
}

// Test 14: a B2C payout is an outflow and cannot be reconciled to an invoice.
func TestReconcileRejectsB2CInvoice(t *testing.T) {
	repo := newFakePaymentRepository()
	svc := NewService(repo, nil, &config.Config{}, &mockInvoicesService{}, nil, nil, nil)

	businessID := uuid.New()
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		CheckoutRequestID:    "CONV-B2C-REVIEW",
		MpesaReceiptNumber:   "B2C-REVIEW-1",
		Amount:               7000,
		Status:               StatusCompleted,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	if err := svc.ReconcileReviewItem(businessID, tx.ID, ReconcileInput{
		Source: sourceMpesaTestConst, TargetType: targetInvoiceTestConst, TargetID: uuid.New().String(),
	}); !errors.Is(err, ErrTargetDirectionMismatch) {
		t.Fatalf("B2C -> invoice error = %v, want ErrTargetDirectionMismatch", err)
	}
}

func TestParseMoneyStringSignAndMagnitude(t *testing.T) {
	// Normal numbers
	if v, err := parseMoneyString("0.29"); err != nil || v != 29 {
		t.Fatalf("parse 0.29: got %v, %v, want 29, nil", v, err)
	}
	if v, err := parseMoneyString("-1.00"); err != nil || v != -100 {
		t.Fatalf("parse -1.00: got %v, %v, want -100, nil", v, err)
	}
	// Double signs must be rejected
	if _, err := parseMoneyString("--1.00"); err == nil {
		t.Fatal("parse --1.00 should fail")
	}
	if _, err := parseMoneyString("-+1.00"); err == nil {
		t.Fatal("parse -+1.00 should fail")
	}
	if _, err := parseMoneyString("+-1.00"); err == nil {
		t.Fatal("parse +-1.00 should fail")
	}
	// Letters and invalid characters
	if _, err := parseMoneyString("abc"); err == nil {
		t.Fatal("parse abc should fail")
	}
	// Max magnitude overflow
	if _, err := parseMoneyString("92233720368547758.08"); err == nil {
		t.Fatal("magnitude exceeding MaxInt64 cents should fail")
	}
}

func TestValidateCallbackSignatureRejectsEmptySecret(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	// A matching signature over an empty or whitespace-only secret must still be
	// rejected; a nonblank secret keeps its matching signature as the control.
	if ValidateCallbackSignature("", body, computeHMAC("", body)) {
		t.Fatal("empty secret must reject a matching signature")
	}
	if ValidateCallbackSignature("   ", body, computeHMAC("   ", body)) {
		t.Fatal("whitespace secret must reject a matching signature")
	}
	if !ValidateCallbackSignature("secret", body, computeHMAC("secret", body)) {
		t.Fatal("nonblank secret must accept its matching signature")
	}
}

func TestIdempotencyKeyConflictDetection(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	// Initial STK push
	stkTx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		PhoneNumber:          "254712345678",
		Amount:               5000,
		IdempotencyKey:       "shared-key-1",
		AccountReference:     "BusinessOS",
		Status:               StatusPending,
		DispatchAcknowledged: true,
		CheckoutRequestID:    "ws_CO_EXISTING",
	}
	_ = repo.CreateMpesaTransaction(stkTx)

	// Replay with identical params returns the same transaction
	identical, err := svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         5000,
		IdempotencyKey: "shared-key-1",
	})
	if err != nil || identical == nil || identical.ID != stkTx.ID {
		t.Fatalf("identical replay failed: %v", err)
	}

	// Replay with different amount fails with conflict
	_, err = svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         6000,
		IdempotencyKey: "shared-key-1",
	})
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict, got %v", err)
	}

	// Replay for B2C with the STK key fails with conflict
	_, err = svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         5000,
		IdempotencyKey: "shared-key-1",
	})
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict for cross-type replay, got %v", err)
	}
}

func TestSTKIdempotencyIncludesSettlementTarget(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	seed := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		PhoneNumber:          "254712345678",
		Amount:               5000,
		IdempotencyKey:       "target-key-1",
		AccountReference:     "INV-A",
		Status:               StatusPending,
		DispatchAcknowledged: true,
		CheckoutRequestID:    "ws_CO_TARGET",
	}
	_ = repo.CreateMpesaTransaction(seed)

	// The same settlement target is an identical request and replays safely.
	existing, err := svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber:      PhoneNumberTestConst,
		Amount:           5000,
		AccountReference: "INV-A",
		IdempotencyKey:   "target-key-1",
	})
	if err != nil || existing == nil || existing.ID != seed.ID {
		t.Fatalf("identical target replay failed: %v", err)
	}

	// Reusing the key for a different invoice must not return the first payment.
	_, err = svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber:      PhoneNumberTestConst,
		Amount:           5000,
		AccountReference: "INV-B",
		IdempotencyKey:   "target-key-1",
	})
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict for a different settlement target, got %v", err)
	}
}

func TestTimedOutPayoutCannotBePosted(t *testing.T) {
	repo := newFakePaymentRepository()
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, finSvc, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		Amount:               5000,
		Status:               StatusTimedOut,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	err := svc.ReconcileReviewItem(businessID, tx.ID, ReconcileInput{
		Source:     sourceMpesaTestConst,
		TargetType: string(EntityExpense),
	})
	if !errors.Is(err, ErrTransactionNotCompleted) {
		t.Fatalf("expected timed-out payout posting to be rejected, got %v", err)
	}
	if len(finSvc.expenses) != 0 {
		t.Fatal("a timed-out payout must not create an expense before confirmation")
	}

	// Dismissal is still allowed for a timed-out row.
	if err := svc.ReconcileReviewItem(businessID, tx.ID, ReconcileInput{Source: sourceMpesaTestConst, TargetType: targetDismissTestConst}); err != nil {
		t.Fatalf("dismissing a timed-out payout failed: %v", err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.ReconciliationStatus != Manual {
		t.Fatalf("status = %s, want manual", stored.ReconciliationStatus)
	}
}

func TestBankReconcileRecoversExistingPosting(t *testing.T) {
	repo := newFakePaymentRepository()
	invSvc := &mockInvoicesService{}
	svc := NewService(repo, nil, &config.Config{}, invSvc, nil, nil, nil)
	businessID := uuid.New()

	tx := &BankTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		Reference:            "BANK-RECOVER-1",
		Type:                 BankInflow,
		Amount:               7500,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateBankTransaction(tx)

	// Simulate a payment that committed for invoice A before the link update
	// failed, then a reviewer selects invoice B.
	existingInvoice := uuid.New()
	repo.application = &fakeApplication{
		marker:     bankMarker(tx),
		entityType: EntityInvoice,
		entityID:   existingInvoice,
	}

	err := svc.ReconcileReviewItem(businessID, tx.ID, ReconcileInput{
		Source:     "bank",
		TargetType: targetInvoiceTestConst,
		TargetID:   uuid.New().String(),
	})
	if err != nil {
		t.Fatalf("recovering an existing bank posting failed: %v", err)
	}
	stored, _ := repo.GetBankTransactionByID(tx.ID, businessID)
	if stored.ReconciliationStatus != Reconciled || stored.ReconciledEntityType != EntityInvoice {
		t.Fatalf("expected reconciled to the existing invoice, got %s / %s", stored.ReconciliationStatus, stored.ReconciledEntityType)
	}
	if stored.ReconciledEntityID == nil || *stored.ReconciledEntityID != existingInvoice {
		t.Fatalf("recovered the wrong entity: %v", stored.ReconciledEntityID)
	}
	if invSvc.paidInvoiceID != uuid.Nil {
		t.Fatal("recovery must not post a second invoice payment")
	}
}

// A posting may commit before its reconciliation link update fails. The review
// path must recover that posting by receipt marker instead of returning
// ErrAlreadyReconciled forever, including when the reviewer dismisses the item.
func TestMpesaReviewRecoversCommittedPosting(t *testing.T) {
	cases := []struct {
		name   string
		status TransactionStatus
		target string
	}{
		{"completed invoice", StatusCompleted, targetInvoiceTestConst},
		{"completed dismiss", StatusCompleted, targetDismissTestConst},
		// A timed-out row may still have moved money; recover the posting
		// instead of refusing the item as unpostable.
		{"timed out invoice", StatusTimedOut, targetInvoiceTestConst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakePaymentRepository()
			invSvc := &mockInvoicesService{}
			svc := NewService(repo, nil, &config.Config{}, invSvc, nil, nil, nil)
			businessID := uuid.New()

			tx := &MpesaTransaction{
				ID:                   uuid.New(),
				BusinessID:           businessID,
				TransactionType:      TypeSTKPush,
				MpesaReceiptNumber:   "QK-RECOVER-1",
				Amount:               5000,
				Status:               tc.status,
				ReconciliationStatus: Unmatched,
			}
			_ = repo.CreateMpesaTransaction(tx)

			// The payment committed for this invoice, but the link update failed.
			existingInvoice := uuid.New()
			repo.application = &fakeApplication{
				marker:     mpesaMarker(tx),
				entityType: EntityInvoice,
				entityID:   existingInvoice,
			}

			input := ReconcileInput{Source: sourceMpesaTestConst, TargetType: tc.target}
			if tc.target == targetInvoiceTestConst {
				input.TargetID = uuid.New().String()
			}
			if err := svc.ReconcileReviewItem(businessID, tx.ID, input); err != nil {
				t.Fatalf("recovering a committed mpesa posting failed: %v", err)
			}

			stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
			if stored.ReconciliationStatus != Reconciled || stored.ReconciledEntityType != EntityInvoice {
				t.Fatalf("expected reconciled to the existing invoice, got %s / %s", stored.ReconciliationStatus, stored.ReconciledEntityType)
			}
			if stored.ReconciledEntityID == nil || *stored.ReconciledEntityID != existingInvoice {
				t.Fatalf("recovered the wrong entity: %v", stored.ReconciledEntityID)
			}
			if invSvc.paidInvoiceID != uuid.Nil {
				t.Fatal("recovery must not post a second invoice payment")
			}
		})
	}
}

// A callback that loaded an open row before a reviewer finalized it must not
// clear or replace the finalized reconciliation link.
func TestCallbackReconciliationDoesNotClobberFinalizedLink(t *testing.T) {
	repo := newFakePaymentRepository()
	businessID := uuid.New()
	entityID := uuid.New()
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		MpesaReceiptNumber:   "QK-CLOBBER-1",
		Amount:               5000,
		Status:               StatusCompleted,
		ReconciliationStatus: Reconciled,
		ReconciledEntityType: EntityInvoice,
		ReconciledEntityID:   &entityID,
	}
	_ = repo.CreateMpesaTransaction(tx)

	// The callback loaded the row while it was still unmatched.
	stale := *tx
	stale.ReconciliationStatus = Unmatched
	stale.ReconciledEntityType = ""
	stale.ReconciledEntityID = nil
	if err := repo.UpdateMpesaReconciliation(&stale); err != nil {
		t.Fatalf("guarded reconciliation update failed: %v", err)
	}

	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.ReconciliationStatus != Reconciled || stored.ReconciledEntityType != EntityInvoice {
		t.Fatalf("finalized link was clobbered: %s / %s", stored.ReconciliationStatus, stored.ReconciledEntityType)
	}
	if stored.ReconciledEntityID == nil || *stored.ReconciledEntityID != entityID {
		t.Fatalf("finalized entity was cleared: %v", stored.ReconciledEntityID)
	}
}

func TestSTKCallbackRequiresReceiptAndAmount(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                uuid.New(),
		BusinessID:        businessID,
		TransactionType:   TypeSTKPush,
		CheckoutRequestID: "ws_CO_INCOMPLETE",
		Amount:            5000,
		Status:            StatusPending,
	}
	_ = repo.CreateMpesaTransaction(tx)

	// Missing MpesaReceiptNumber
	bodyNoReceipt := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_INCOMPLETE","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"Amount","Value":50.0}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, bodyNoReceipt), bodyNoReceipt); err == nil {
		t.Fatal("expected error for callback missing receipt")
	}

	// Missing Amount
	bodyNoAmount := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_INCOMPLETE","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"REC123"}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, bodyNoAmount), bodyNoAmount); err == nil {
		t.Fatal("expected error for callback missing amount")
	}

	// Verify transaction remains pending
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusPending {
		t.Fatalf("expected status to remain pending, got %s", stored.Status)
	}
}

func TestC2BConfirmationRequiresTransID(t *testing.T) {
	repo := newFakePaymentRepository()
	businessID := uuid.New()
	// Bind a valid business so the fixture reaches the TransID guard instead of
	// failing earlier with "Could not resolve business".
	cfg := &config.Config{MpesaCallbackSecret: "sec", MpesaBusinessID: businessID.String()}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)

	sig := computeHMAC(cfg.MpesaCallbackSecret, nil)
	resp, err := svc.HandleC2BConfirmation(sig, nil, C2BConfirmationRequest{
		TransID:     "   ",
		TransAmount: "100.00",
		MSISDN:      "254712345678",
	}, uuid.Nil)
	if err == nil || resp.ResultCode == 0 {
		t.Fatal("expected error for empty TransID")
	}
	if !strings.Contains(resp.ResultDesc, "Missing TransID") {
		t.Fatalf("expected a Missing TransID rejection, got %q", resp.ResultDesc)
	}
	if len(repo.mpesaTxs) != 0 {
		t.Fatal("missing TransID must not persist a transaction")
	}

	// Successful control: the same fixture with a nonblank ID is persisted.
	resp, err = svc.HandleC2BConfirmation(sig, nil, C2BConfirmationRequest{
		TransID:     "QK-MISSING-ID-CONTROL",
		TransAmount: "100.00",
		MSISDN:      "254712345678",
	}, uuid.Nil)
	if err != nil || resp.ResultCode != 0 {
		t.Fatalf("control confirmation failed: res=%+v err=%v", resp, err)
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("expected the control confirmation to persist one transaction, got %d", len(repo.mpesaTxs))
	}
}

func TestManualBankDirectionValidation(t *testing.T) {
	repo := newFakePaymentRepository()
	svc := NewService(repo, nil, &config.Config{}, nil, nil, nil, nil)
	businessID := uuid.New()

	_, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference: "REF-UNKNOWN",
		Amount:    1000,
		Type:      "invalid-direction",
	}, true)
	if err == nil {
		t.Fatal("expected error for invalid bank direction")
	}

	// Inflow and withdrawal recognized
	inflowTx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference: "REF-INFLOW",
		Amount:    1000,
		Type:      "deposit",
	}, true)
	if err != nil || inflowTx.Type != BankInflow {
		t.Fatalf("expected deposit to normalize to BankInflow: %v", err)
	}

	outflowTx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference: "REF-OUTFLOW",
		Amount:    1000,
		Type:      "withdrawal",
	}, true)
	if err != nil || outflowTx.Type != BankOutflow {
		t.Fatalf("expected withdrawal to normalize to BankOutflow: %v", err)
	}
}

func TestStatementImportValidationAndInvalidRows(t *testing.T) {
	repo := newFakePaymentRepository()
	svc := NewService(repo, nil, &config.Config{}, nil, nil, nil, nil)
	businessID := uuid.New()

	// Missing amount column
	noAmountCSV := strings.NewReader("date,reference,description\n2026-11-01,REF1,desc\n")
	_, err := svc.ImportBankStatement(businessID, "TestBank", noAmountCSV)
	if err == nil {
		t.Fatal("expected error when amount column is missing")
	}

	// CSV with 1 valid row, 1 duplicate, 1 row missing amount, 1 row missing reference
	csvData := `date,reference,amount,description
2026-11-01,REF-OK,100.00,valid payment
2026-11-01,REF-NO-AMOUNT,,missing amount
2026-11-01,,50.00,missing ref
2026-11-01,REF-OK,100.00,duplicate
`
	result, err := svc.ImportBankStatement(businessID, "TestBank", strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1", result.Imported)
	}
	if result.SkippedDuplicates != 1 {
		t.Errorf("skipped_duplicates = %d, want 1", result.SkippedDuplicates)
	}
	if result.InvalidRows != 2 {
		t.Errorf("invalid_rows = %d, want 2", result.InvalidRows)
	}

	// A nonblank but unparseable credit must invalidate the row instead of
	// letting the debit side import it as a different movement.
	invalidCreditCSV := `date,reference,credit,debit
2026-11-02,REF-BAD-CREDIT,broken,25.00
`
	result, err = svc.ImportBankStatement(businessID, "TestBank", strings.NewReader(invalidCreditCSV))
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Imported != 0 || result.InvalidRows != 1 {
		t.Errorf("imported = %d, invalid_rows = %d; want 0 and 1", result.Imported, result.InvalidRows)
	}
	if _, err := repo.GetBankTransactionByReference(businessID, "REF-BAD-CREDIT"); err == nil {
		t.Fatal("a row with a broken credit must not be imported")
	}
}

func TestPaginationBounds(t *testing.T) {
	repo := newFakePaymentRepository()
	svc := NewService(repo, nil, &config.Config{}, nil, nil, nil, nil)
	businessID := uuid.New()

	// Extremely large page that would overflow int
	hugePage := math.MaxInt
	_, _, err := svc.ListReviewQueue(businessID, "", hugePage, 20)
	if err == nil {
		t.Fatal("expected error for page causing offset overflow")
	}
}

// A statement row that lists both a credit and a debit is ambiguous and must
// not be imported as a single movement.
func TestStatementImportRejectsCreditAndDebit(t *testing.T) {
	repo := newFakePaymentRepository()
	svc := NewService(repo, nil, &config.Config{}, nil, nil, nil, nil)
	businessID := uuid.New()

	csv := `date,reference,credit,debit
2026-11-01,REF-BOTH,100.00,25.00
2026-11-02,REF-OK,100.00,
`
	result, err := svc.ImportBankStatement(businessID, "TestBank", strings.NewReader(csv))
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.InvalidRows != 1 {
		t.Errorf("invalid_rows = %d, want 1", result.InvalidRows)
	}
	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1", result.Imported)
	}
}

// A B2C result without an explicit ResultCode must not complete the payout.
func TestB2CResultRequiresResultCode(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, &mockFinanceService{}, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		CheckoutRequestID:    "CONV-NO-CODE",
		Amount:               1000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Result":{"ResultDesc":"ok","ConversationID":"CONV-NO-CODE","TransactionID":"X"}}`)
	if err := svc.HandleB2CResult(computeHMAC(cfg.MpesaCallbackSecret, body), body); err == nil {
		t.Fatal("expected error when ResultCode is missing")
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusPending {
		t.Fatalf("status = %s, want pending", stored.Status)
	}
}

// An ambiguous STK outcome must leave the transaction pending so a late
// callback can still settle it.
func TestInitiateSTKAmbiguousKeepsPending(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		stkResp: &DarajaSTKPushResponse{CheckoutRequestID: "ws_CO_AMB", ResponseCode: "0"},
		stkErr:  ErrTransactionAmbiguous,
	}
	cfg := &config.Config{
		MpesaShortCode:      "174379",
		MpesaPasskey:        passkeyTestConst,
		MpesaStkCallbackURL: "https://example.com/callback",
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	businessID := uuid.New()
	input := STKPushInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         1000,
		IdempotencyKey: "stk-amb-pending-1",
	}
	if _, err := svc.InitiateSTKPush(context.Background(), businessID, input); err == nil {
		t.Fatal("expected ambiguous error to surface")
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("expected exactly one stored reservation, got %d", len(repo.mpesaTxs))
	}
	for _, m := range repo.mpesaTxs {
		if m.Status != StatusPending {
			t.Fatalf("status = %s, want pending", m.Status)
		}
		if m.CheckoutRequestID != "ws_CO_AMB" {
			t.Fatalf("checkout id = %q, want the provider id ws_CO_AMB", m.CheckoutRequestID)
		}
	}

	// A retry with the same key must not resend, and must keep reporting the
	// dispatch as unresolved until the provider acknowledges or a result lands.
	if _, err := svc.InitiateSTKPush(context.Background(), businessID, input); !errors.Is(err, ErrTransactionAmbiguous) {
		t.Fatalf("retry error = %v, want ErrTransactionAmbiguous", err)
	}
	if client.stkCalls != 1 {
		t.Fatalf("gateway calls = %d, want 1", client.stkCalls)
	}

	// Control: an acknowledged dispatch still replays without another call.
	client.stkErr = nil
	client.stkResp = &DarajaSTKPushResponse{CheckoutRequestID: "ws_CO_ACK", ResponseCode: "0"}
	okInput := input
	okInput.IdempotencyKey = "stk-amb-ack"
	first, err := svc.InitiateSTKPush(context.Background(), businessID, okInput)
	if err != nil {
		t.Fatalf("acknowledged dispatch failed: %v", err)
	}
	replay, err := svc.InitiateSTKPush(context.Background(), businessID, okInput)
	if err != nil || replay == nil || replay.ID != first.ID {
		t.Fatalf("acknowledged replay failed: %v", err)
	}
	if client.stkCalls != 2 {
		t.Fatalf("gateway calls = %d, want 2 (the replay must not call)", client.stkCalls)
	}
}

// An ambiguous B2C outcome must move to timed_out (non-final) rather than failed.
func TestInitiateB2CAmbiguousUsesTimedOut(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		b2cResp: &DarajaB2CResponse{ConversationID: "CONV-AMB", ResponseCode: "0"},
		b2cErr:  ErrTransactionAmbiguous,
	}
	cfg := &config.Config{MpesaShortCode: "174379", MpesaPasskey: passkeyTestConst}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	businessID := uuid.New()
	input := B2CPayoutInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         1000,
		IdempotencyKey: "b2c-amb-1",
	}
	if _, err := svc.InitiateB2C(context.Background(), businessID, input); err == nil {
		t.Fatal("expected ambiguous error to surface")
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("expected exactly one stored reservation, got %d", len(repo.mpesaTxs))
	}
	for _, m := range repo.mpesaTxs {
		if m.Status != StatusTimedOut {
			t.Fatalf("status = %s, want timed_out", m.Status)
		}
		if m.CheckoutRequestID != "CONV-AMB" {
			t.Fatalf("conversation id = %q, want the provider id CONV-AMB", m.CheckoutRequestID)
		}
	}

	// A retry with the same key must not resend, and must keep reporting the
	// dispatch as unresolved until the provider acknowledges or a result lands.
	if _, err := svc.InitiateB2C(context.Background(), businessID, input); !errors.Is(err, ErrTransactionAmbiguous) {
		t.Fatalf("retry error = %v, want ErrTransactionAmbiguous", err)
	}
	if client.b2cCalls != 1 {
		t.Fatalf("gateway calls = %d, want 1", client.b2cCalls)
	}

	// Control: an acknowledged dispatch still replays without another call.
	client.b2cErr = nil
	client.b2cResp = &DarajaB2CResponse{ConversationID: "CONV-ACK", ResponseCode: "0"}
	okInput := input
	okInput.IdempotencyKey = "b2c-amb-ack"
	first, err := svc.InitiateB2C(context.Background(), businessID, okInput)
	if err != nil {
		t.Fatalf("acknowledged dispatch failed: %v", err)
	}
	replay, err := svc.InitiateB2C(context.Background(), businessID, okInput)
	if err != nil || replay == nil || replay.ID != first.ID {
		t.Fatalf("acknowledged replay failed: %v", err)
	}
	if client.b2cCalls != 2 {
		t.Fatalf("gateway calls = %d, want 2 (the replay must not call)", client.b2cCalls)
	}
}

// A signed STK callback without an explicit ResultCode must not be treated as
// success (a missing integer would otherwise decode to zero).
func TestSTKCallbackRequiresResultCode(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		CheckoutRequestID:    "ws_CO_NO_CODE",
		Amount:               5000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_NO_CODE","ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"R1"},{"Name":"Amount","Value":50.0}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err == nil {
		t.Fatal("expected error when ResultCode is missing")
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusPending {
		t.Fatalf("status = %s, want pending", stored.Status)
	}
}

// A proven pre-dispatch failure must release the idempotency reservation so a
// retry can dispatch instead of replaying a stuck pending row.
func TestInitiateSTKPreDispatchReleasesReservation(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		stkErr: fmt.Errorf("%w: oauth unavailable", ErrMpesaPreDispatch),
	}
	cfg := &config.Config{
		MpesaShortCode:      "174379",
		MpesaPasskey:        passkeyTestConst,
		MpesaStkCallbackURL: "https://example.com/callback",
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	if _, err := svc.InitiateSTKPush(context.Background(), uuid.New(), STKPushInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         1000,
		IdempotencyKey: "pre-dispatch-1",
	}); err == nil {
		t.Fatal("expected pre-dispatch error to surface")
	}
	if len(repo.mpesaTxs) != 0 {
		t.Fatalf("reservation should be released, %d rows remain", len(repo.mpesaTxs))
	}
}

// Provider callback URLs must carry the dedicated token, not the HMAC secret.
func TestCallbackURLUsesDedicatedToken(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{
		stkResp: &DarajaSTKPushResponse{CheckoutRequestID: "ws_CO_TOKEN", ResponseCode: "0"},
	}
	cfg := &config.Config{
		BackendURL:          "https://api.example.com",
		MpesaCallbackSecret: "signing-secret",
		MpesaCallbackToken:  "public-callback-token",
		MpesaShortCode:      "174379",
		MpesaPasskey:        passkeyTestConst,
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	if _, err := svc.InitiateSTKPush(context.Background(), uuid.New(), STKPushInput{
		PhoneNumber: PhoneNumberTestConst,
		Amount:      1000,
	}); err != nil {
		t.Fatalf("InitiateSTKPush failed: %v", err)
	}
	got := client.lastReq.CallBackURL
	if !strings.Contains(got, "token=public-callback-token") {
		t.Fatalf("callback URL %q does not embed the dedicated token", got)
	}
	if strings.Contains(got, "signing-secret") {
		t.Fatalf("callback URL %q leaks the signing secret", got)
	}
}

// A numeric Daraja phone value must be stored normalized, not in scientific
// notation ("%v" on a float64).
func TestSTKCallbackNormalizesNumericPhone(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "phone-secret"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		CheckoutRequestID:    "ws_CO_PHONE",
		Amount:               5000,
		Status:               StatusPending,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_PHONE","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"PHONE1"},{"Name":"Amount","Value":50.0},{"Name":"PhoneNumber","Value":254708374149}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.PhoneNumber != "254708374149" {
		t.Fatalf("phone = %q, want 254708374149", stored.PhoneNumber)
	}
}

// A C2B validation that cannot attribute the payment must reject it instead of
// letting Daraja capture money that confirmation would drop.
func TestC2BValidationRejectsUnknownAccount(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "c2b-secret"}
	svc := NewService(repo, nil, cfg, nil, &mockCustomersService{}, nil, nil)

	req := C2BValidationRequest{
		TransAmount:   "250.00",
		MSISDN:        "254799999999",
		BillRefNumber: "NO-SUCH-INVOICE",
	}
	res, err := svc.HandleC2BValidation(DeriveCallbackToken("c2b-secret"), nil, req)
	if err != nil {
		t.Fatalf("validation returned error: %v", err)
	}
	if res.ResultCode == 0 {
		t.Fatal("expected unknown account to be rejected at validation")
	}
}

// The automatic bank path must tag its payment note with the bank marker so a
// later manual review can dedupe instead of booking the money twice.
func TestAutoReconcileBankTagsInvoiceNoteWithMarker(t *testing.T) {
	repo := newFakePaymentRepository()
	invSvc := &mockInvoicesService{}
	svc := NewService(repo, nil, &config.Config{}, invSvc, nil, nil, nil)

	businessID := uuid.New()
	invID := uuid.New()
	repo.invoices["INV-BANK-1"] = &invoices.Invoice{
		ID:            invID,
		BusinessID:    businessID,
		InvoiceNumber: "INV-BANK-1",
		TotalAmount:   5000,
		Status:        invoices.StatusUnpaid,
	}

	tx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		BankName:  "Equity",
		Reference: "INV-BANK-1",
		Type:      "deposit",
		Amount:    5000,
	}, true)
	if err != nil {
		t.Fatalf("manual transfer failed: %v", err)
	}
	if tx.ReconciliationStatus != Reconciled {
		t.Fatalf("status = %s, want reconciled", tx.ReconciliationStatus)
	}
	if len(invSvc.payments) != 1 {
		t.Fatalf("expected 1 invoice payment, got %d", len(invSvc.payments))
	}
	if !strings.HasPrefix(invSvc.payments[0].Note, bankMarker(tx)) {
		t.Fatalf("payment note %q is not tagged with %q", invSvc.payments[0].Note, bankMarker(tx))
	}
}

// An ambiguous STK dispatch with no response must land in a recoverable
// timed-out state, surface in the review queue, and not masquerade as a
// successful replay.
func TestInitiateSTKAmbiguousNetworkUsesTimedOut(t *testing.T) {
	repo := newFakePaymentRepository()
	client := &mockMpesaClient{stkErr: ErrMpesaClientFailed}
	cfg := &config.Config{
		MpesaShortCode:      "174379",
		MpesaPasskey:        passkeyTestConst,
		MpesaStkCallbackURL: "https://example.com/cb",
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)
	businessID := uuid.New()
	input := STKPushInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         1000,
		IdempotencyKey: "amb-1",
	}

	if _, err := svc.InitiateSTKPush(context.Background(), businessID, input); err == nil {
		t.Fatal("expected ambiguous dispatch error")
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("expected exactly one stored reservation, got %d", len(repo.mpesaTxs))
	}
	for _, m := range repo.mpesaTxs {
		if m.Status != StatusTimedOut {
			t.Fatalf("status = %s, want timed_out", m.Status)
		}
	}

	// A replay with the same key must not look like a sent prompt.
	if _, err := svc.InitiateSTKPush(context.Background(), businessID, input); err == nil {
		t.Fatal("expected unresolved replay to report an error")
	}
	if client.stkCalls != 1 {
		t.Fatalf("gateway calls = %d, want 1", client.stkCalls)
	}

	items, total, err := svc.ListReviewQueue(businessID, sourceMpesaTestConst, 1, 10)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 review item for the timed-out STK, got %d (err %v)", total, err)
	}
}

// A timed-out STK push is not final: the prompt may still reach the customer,
// so a late success callback must resolve it instead of being dropped.
func TestLateSTKCallbackResolvesTimedOut(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		CheckoutRequestID:    "ws_CO_LATE",
		Amount:               5000,
		Status:               StatusTimedOut,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)

	body := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_LATE","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"QK-LATE-1"},{"Name":"Amount","Value":50.0},{"Name":"PhoneNumber","Value":"254712345678"}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("late callback failed: %v", err)
	}

	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", stored.Status)
	}
	if stored.MpesaReceiptNumber != "QK-LATE-1" {
		t.Fatalf("receipt = %q, want QK-LATE-1", stored.MpesaReceiptNumber)
	}
}

// A proven pre-dispatch failure whose reservation cleanup also fails must stay
// retryable: one replay clears the marker and dispatches exactly once.
func TestInitiateSTKPreDispatchCleanupFailureRemainsRetryable(t *testing.T) {
	repo := newFakePaymentRepository()
	repo.deleteErr = errors.New("database unavailable")
	client := &mockMpesaClient{stkErr: fmt.Errorf("%w: oauth unavailable", ErrMpesaPreDispatch)}
	cfg := &config.Config{
		MpesaShortCode:      "174379",
		MpesaPasskey:        passkeyTestConst,
		MpesaStkCallbackURL: "https://example.com/callback",
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)
	businessID := uuid.New()
	input := STKPushInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         1000,
		IdempotencyKey: "predispatch-cleanup-1",
	}

	if _, err := svc.InitiateSTKPush(context.Background(), businessID, input); err == nil {
		t.Fatal("expected the pre-dispatch error to surface")
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("expected the reservation to remain retryable, got %d rows", len(repo.mpesaTxs))
	}
	for _, m := range repo.mpesaTxs {
		if !retryablePreDispatch(m) {
			t.Fatalf("row = %s / %q, want a retryable pre-dispatch marker", m.Status, m.ResultDesc)
		}
	}

	// Once cleanup works, the same key dispatches instead of blocking forever.
	repo.deleteErr = nil
	client.stkErr = nil
	client.stkResp = &DarajaSTKPushResponse{CheckoutRequestID: "ws_CO_RETRY", ResponseCode: "0"}
	tx, err := svc.InitiateSTKPush(context.Background(), businessID, input)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if !tx.DispatchAcknowledged {
		t.Fatal("retry should record the acknowledged dispatch")
	}
	if client.stkCalls != 2 {
		t.Fatalf("gateway calls = %d, want 2 (one failed attempt, one retry)", client.stkCalls)
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("reservations = %d, want exactly one after the retry", len(repo.mpesaTxs))
	}
}

// A supplier payout may not exceed the payable after in-flight payouts.
func TestB2CSupplierPayoutRejectsOverpayment(t *testing.T) {
	repo := newFakePaymentRepository()
	supSvc := &mockSuppliersService{outstanding: 10000}
	client := &mockMpesaClient{
		b2cResp: &DarajaB2CResponse{ConversationID: "CONV-OVER", ResponseCode: "0"},
	}
	svc := NewService(repo, client, &config.Config{MpesaShortCode: "174379", MpesaPasskey: passkeyTestConst}, nil, nil, nil, supSvc)
	businessID := uuid.New()
	supplierID := uuid.New()
	supSvc.paidSupplierID = supplierID

	if _, err := svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber: PhoneNumberTestConst,
		Amount:      15000,
		SupplierID:  supplierID.String(),
	}); !errors.Is(err, ErrPayoutExceedsPayable) {
		t.Fatalf("error = %v, want ErrPayoutExceedsPayable", err)
	}
	if client.b2cCalls != 0 || len(repo.mpesaTxs) != 0 {
		t.Fatal("an overpayment must not reserve or dispatch")
	}

	// An in-flight payout reserves part of the balance.
	_ = repo.CreateMpesaTransaction(&MpesaTransaction{
		ID:              uuid.New(),
		BusinessID:      businessID,
		TransactionType: TypeB2C,
		Amount:          6000,
		Status:          StatusPending,
		SupplierID:      &supplierID,
	})
	if _, err := svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber: PhoneNumberTestConst,
		Amount:      5000,
		SupplierID:  supplierID.String(),
	}); !errors.Is(err, ErrPayoutExceedsPayable) {
		t.Fatalf("pending reservation error = %v, want ErrPayoutExceedsPayable", err)
	}
	if client.b2cCalls != 0 {
		t.Fatal("the pending reservation must be counted before dispatch")
	}
}

// Dismissal must not hide a payout that completed while the reviewer held the
// claim; the mismatched status rejects Manual and the claim is released.
func TestDismissalCannotHideConcurrentCompletion(t *testing.T) {
	repo := newFakePaymentRepository()
	businessID := uuid.New()
	claimID := uuid.New()
	tx := &MpesaTransaction{
		ID:                    uuid.New(),
		BusinessID:            businessID,
		TransactionType:       TypeB2C,
		Amount:                5000,
		Status:                StatusCompleted,
		ReconciliationStatus:  Claiming,
		ReconciliationClaimID: &claimID,
	}
	_ = repo.CreateMpesaTransaction(tx)

	ok, err := repo.FinalizeMpesaDismissal(tx, claimID, StatusTimedOut)
	if err != nil || ok {
		t.Fatalf("dismissal = %v, %v; want a rejected write", ok, err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.ReconciliationStatus != Claiming {
		t.Fatalf("status = %s, want claiming (released for review)", stored.ReconciliationStatus)
	}

	// Control: the status observed at claim time still commits Manual.
	ok, err = repo.FinalizeMpesaDismissal(tx, claimID, StatusCompleted)
	if err != nil || !ok {
		t.Fatalf("control dismissal = %v, %v; want committed", ok, err)
	}
	if stored.ReconciliationStatus != Manual {
		t.Fatalf("status = %s, want manual", stored.ReconciliationStatus)
	}
}

// A cleanup failure must be surfaced from the queue and the review path rather
// than being mistaken for an already-reconciled item.
func TestStaleClaimCleanupFailureIsSurfaced(t *testing.T) {
	repo := newFakePaymentRepository()
	injected := errors.New("database unavailable")
	repo.releaseErr = injected
	svc := NewService(repo, nil, &config.Config{}, nil, nil, nil, nil)
	businessID := uuid.New()

	if _, _, err := svc.ListReviewQueue(businessID, "", 1, 20); !errors.Is(err, injected) {
		t.Fatalf("ListReviewQueue error = %v, want the injected cleanup failure", err)
	}

	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		Amount:               5000,
		Status:               StatusCompleted,
		ReconciliationStatus: Unmatched,
	}
	_ = repo.CreateMpesaTransaction(tx)
	if err := svc.ReconcileReviewItem(businessID, tx.ID, ReconcileInput{
		Source: sourceMpesaTestConst, TargetType: targetDismissTestConst,
	}); !errors.Is(err, injected) {
		t.Fatalf("ReconcileReviewItem error = %v, want the injected cleanup failure", err)
	}
}

// withSearchPath overrides search_path on a URL-style PostgreSQL DSN so an
// existing value cannot win over the disposable test schema.
func withSearchPath(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("TEST_DATABASE_URL must be a postgres URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// The status predicate on the guarded reconciliation writes must live in SQL,
// not only in the in-memory fake.
func TestReconciliationGuardsAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	schema := fmt.Sprintf("payments_guard_%d", time.Now().UnixNano())
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open configured test database: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error })

	// Override the search path on the connection URL instead of appending a
	// second parameter: pgx takes the first value, so an existing
	// search_path=public would otherwise win and fixtures would land in public.
	scopedDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatalf("set search path: %v", err)
	}
	scoped, err := gorm.Open(postgres.Open(scopedDSN), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open scoped database: %v", err)
	}
	var current string
	if err := scoped.Raw("SELECT current_schema()").Scan(&current).Error; err != nil {
		t.Fatalf("read current schema: %v", err)
	}
	if current != schema {
		t.Fatalf("current_schema = %q, want the disposable schema %q", current, schema)
	}
	if err := scoped.AutoMigrate(&MpesaTransaction{}, &suppliers.Supplier{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := &repository{db: scoped}

	businessID := uuid.New()
	entityID := uuid.New()
	finalized := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		Status:               StatusCompleted,
		ReconciliationStatus: Reconciled,
		ReconciledEntityType: EntityInvoice,
		ReconciledEntityID:   &entityID,
	}
	if err := repo.CreateMpesaTransaction(finalized); err != nil {
		t.Fatalf("seed finalized row: %v", err)
	}
	stale := *finalized
	stale.ReconciliationStatus = Unmatched
	stale.ReconciledEntityType = ""
	stale.ReconciledEntityID = nil
	if err := repo.UpdateMpesaReconciliation(&stale); err != nil {
		t.Fatalf("guarded update: %v", err)
	}
	var got MpesaTransaction
	if err := scoped.First(&got, "id = ?", finalized.ID).Error; err != nil {
		t.Fatalf("read finalized row: %v", err)
	}
	if got.ReconciliationStatus != Reconciled || got.ReconciledEntityType != EntityInvoice ||
		got.ReconciledEntityID == nil || *got.ReconciledEntityID != entityID {
		t.Fatalf("finalized link was clobbered: %+v", got)
	}

	// Control: an open row must still accept a normal settlement update.
	open := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeSTKPush,
		Status:               StatusCompleted,
		ReconciliationStatus: Unmatched,
	}
	if err := repo.CreateMpesaTransaction(open); err != nil {
		t.Fatalf("seed open row: %v", err)
	}
	open.ReconciliationStatus = Reconciled
	open.ReconciledEntityType = EntityInvoice
	open.ReconciledEntityID = &entityID
	if err := repo.UpdateMpesaReconciliation(open); err != nil {
		t.Fatalf("open update: %v", err)
	}
	got = MpesaTransaction{}
	if err := scoped.First(&got, "id = ?", open.ID).Error; err != nil {
		t.Fatalf("read open row: %v", err)
	}
	if got.ReconciliationStatus != Reconciled {
		t.Fatalf("open row status = %s, want reconciled", got.ReconciliationStatus)
	}

	// The dismissal status predicate must also be enforced in SQL.
	claimID := uuid.New()
	claimed := &MpesaTransaction{
		ID:                    uuid.New(),
		BusinessID:            businessID,
		TransactionType:       TypeB2C,
		Status:                StatusCompleted,
		ReconciliationStatus:  Claiming,
		ReconciliationClaimID: &claimID,
	}
	if err := repo.CreateMpesaTransaction(claimed); err != nil {
		t.Fatalf("seed claimed row: %v", err)
	}
	ok, err := repo.FinalizeMpesaDismissal(claimed, claimID, StatusTimedOut)
	if err != nil || ok {
		t.Fatalf("mismatched dismissal = %v, %v; want rejected", ok, err)
	}
	got = MpesaTransaction{}
	if err := scoped.First(&got, "id = ?", claimed.ID).Error; err != nil {
		t.Fatalf("read claimed row: %v", err)
	}
	if got.ReconciliationStatus != Claiming {
		t.Fatalf("claimed row status = %s, want claiming", got.ReconciliationStatus)
	}
	ok, err = repo.FinalizeMpesaDismissal(claimed, claimID, StatusCompleted)
	if err != nil || !ok {
		t.Fatalf("matching dismissal = %v, %v; want committed", ok, err)
	}
	got = MpesaTransaction{}
	if err := scoped.First(&got, "id = ?", claimed.ID).Error; err != nil {
		t.Fatalf("read dismissed row: %v", err)
	}
	if got.ReconciliationStatus != Manual {
		t.Fatalf("dismissed status = %s, want manual", got.ReconciliationStatus)
	}
	if got.ReconciliationClaimID != nil {
		t.Fatalf("dismissed row kept its claim id: %v", got.ReconciliationClaimID)
	}

	// The supplier payable check and the in-flight sum must also hold in SQL.
	supplier := suppliers.Supplier{ID: uuid.New(), BusinessID: businessID, Name: "Supplier", OutstandingBalance: 10000}
	if err := scoped.Create(&supplier).Error; err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	supplierID := supplier.ID
	over := &MpesaTransaction{
		ID: uuid.New(), BusinessID: businessID, TransactionType: TypeB2C,
		Amount: 15000, Status: StatusPending, SupplierID: &supplierID,
	}
	if err := repo.CreateSupplierPayout(over); !errors.Is(err, ErrPayoutExceedsPayable) {
		t.Fatalf("overpayment error = %v, want ErrPayoutExceedsPayable", err)
	}
	payout := &MpesaTransaction{
		ID: uuid.New(), BusinessID: businessID, TransactionType: TypeB2C,
		Amount: 5000, Status: StatusPending, SupplierID: &supplierID,
	}
	if err := repo.CreateSupplierPayout(payout); err != nil {
		t.Fatalf("valid payout: %v", err)
	}
	if total, err := repo.PendingSupplierPayoutTotal(businessID, supplierID); err != nil || total != 5000 {
		t.Fatalf("pending total = %d, %v; want 5000", total, err)
	}
	second := &MpesaTransaction{
		ID: uuid.New(), BusinessID: businessID, TransactionType: TypeB2C,
		Amount: 6000, Status: StatusPending, SupplierID: &supplierID,
	}
	if err := repo.CreateSupplierPayout(second); !errors.Is(err, ErrPayoutExceedsPayable) {
		t.Fatalf("reserved overpayment error = %v, want ErrPayoutExceedsPayable", err)
	}

	// A confirmed payout remains reserved until its supplier payment is linked.
	if err := scoped.Model(&MpesaTransaction{}).Where("id = ?", payout.ID).
		Updates(map[string]any{"status": StatusCompleted, "reconciled_entity_id": nil}).Error; err != nil {
		t.Fatalf("complete the first payout: %v", err)
	}
	if total, err := repo.PendingSupplierPayoutTotal(businessID, supplierID); err != nil || total != 5000 {
		t.Fatalf("completed-unposted total = %d, %v; want 5000", total, err)
	}
	afterCompletion := &MpesaTransaction{
		ID: uuid.New(), BusinessID: businessID, TransactionType: TypeB2C,
		Amount: 6000, Status: StatusPending, SupplierID: &supplierID,
	}
	if err := repo.CreateSupplierPayout(afterCompletion); !errors.Is(err, ErrPayoutExceedsPayable) {
		t.Fatalf("completed-unposted overpayment error = %v, want ErrPayoutExceedsPayable", err)
	}
}

// Replaying a supplier payout must not count its own pending reservation as a
// competing payout and reject the retry as an overpayment.
func TestB2CSupplierPayoutReplayDoesNotCountItsOwnReservation(t *testing.T) {
	repo := newFakePaymentRepository()
	supSvc := &mockSuppliersService{outstanding: 10000}
	client := &mockMpesaClient{
		b2cResp: &DarajaB2CResponse{ConversationID: "CONV-SUP-FULL", ResponseCode: "0"},
	}
	svc := NewService(repo, client, &config.Config{MpesaShortCode: "174379", MpesaPasskey: passkeyTestConst}, nil, nil, nil, supSvc)
	businessID := uuid.New()
	supplierID := uuid.New()
	supSvc.paidSupplierID = supplierID
	input := B2CPayoutInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         10000,
		SupplierID:     supplierID.String(),
		IdempotencyKey: "supplier-full-1",
	}

	first, err := svc.InitiateB2C(context.Background(), businessID, input)
	if err != nil {
		t.Fatalf("first payout failed: %v", err)
	}
	replay, err := svc.InitiateB2C(context.Background(), businessID, input)
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay ID = %s, want %s", replay.ID, first.ID)
	}
	if client.b2cCalls != 1 {
		t.Fatalf("gateway calls = %d, want 1", client.b2cCalls)
	}
}

// An outflow without an explicit expense intent stays unmatched even when the
// caller holds FinanceManage, so a supplier transfer can be allocated in review.
func TestBankOutflowWithoutIntentStaysUnmatched(t *testing.T) {
	repo := newFakePaymentRepository()
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, finSvc, nil)
	businessID := uuid.New()

	tx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference:   "OUT-UNASSIGNED",
		Type:        "withdrawal",
		Amount:      4000,
		Description: "Transfer to supplier",
	}, true)
	if err != nil {
		t.Fatalf("recording the transfer failed: %v", err)
	}
	if tx.ReconciliationStatus != Unmatched {
		t.Fatalf("status = %s, want unmatched", tx.ReconciliationStatus)
	}
	if len(finSvc.expenses) != 0 {
		t.Fatal("an unassigned outflow must not post an operating expense")
	}
}

// A signed STK success whose checkout id matches no reservation (for example
// after a lost initiation response) must be preserved for review.
func TestUnmatchedSTKCallbackIsPreserved(t *testing.T) {
	repo := newFakePaymentRepository()
	businessID := uuid.New()
	cfg := &config.Config{MpesaCallbackSecret: "sec", MpesaBusinessID: businessID.String()}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)

	body := []byte(`{"Body":{"stkCallback":{"CheckoutRequestID":"ws_CO_LOST","ResultCode":0,"ResultDesc":"ok","CallbackMetadata":{"Item":[{"Name":"MpesaReceiptNumber","Value":"QK-LOST-1"},{"Name":"Amount","Value":50.0},{"Name":"PhoneNumber","Value":"254712345678"}]}}}}`)
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	stored, err := repo.GetMpesaTransactionByReceipt("QK-LOST-1")
	if err != nil {
		t.Fatalf("the receipt was not preserved: %v", err)
	}
	if stored.BusinessID != businessID || stored.Status != StatusCompleted || stored.ReconciliationStatus != Unmatched {
		t.Fatalf("preserved row = %s/%s for %s, want completed/unmatched for %s",
			stored.Status, stored.ReconciliationStatus, stored.BusinessID, businessID)
	}
	if stored.CheckoutRequestID != "ws_CO_LOST" {
		t.Fatalf("checkout id = %q, want ws_CO_LOST", stored.CheckoutRequestID)
	}

	// A redelivery must not create a second row.
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("duplicate callback failed: %v", err)
	}
	if len(repo.mpesaTxs) != 1 {
		t.Fatalf("rows = %d, want 1", len(repo.mpesaTxs))
	}
}

// A concurrent identical payout that consumed the payable between lookup and
// check must return its existing dispatch instead of a payable error.
func TestPayableRejectionReturnsExistingIdenticalDispatch(t *testing.T) {
	repo := newFakePaymentRepository()
	supSvc := &mockSuppliersService{outstanding: 10000}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, nil, supSvc)
	businessID := uuid.New()
	supplierID := uuid.New()
	supSvc.paidSupplierID = supplierID

	existing := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		PhoneNumber:          "254712345678",
		Amount:               10000,
		Status:               StatusPending,
		DispatchAcknowledged: true,
		IdempotencyKey:       "concurrent-payable-1",
		SupplierID:           &supplierID,
		ExpenseCategory:      defaultExpenseCategory,
	}
	_ = repo.CreateMpesaTransaction(existing)

	input := B2CPayoutInput{
		PhoneNumber:    PhoneNumberTestConst,
		Amount:         10000,
		SupplierID:     supplierID.String(),
		IdempotencyKey: "concurrent-payable-1",
	}
	impl, ok := svc.(*service)
	if !ok {
		t.Fatal("expected the concrete payment service")
	}
	got, err := impl.resolvePayableRejection(businessID, input, NormalizePhone(input.PhoneNumber), &supplierID, defaultExpenseCategory)
	if err != nil {
		t.Fatalf("expected the existing dispatch, got %v", err)
	}
	if got == nil || got.ID != existing.ID {
		t.Fatalf("returned %+v, want the existing reservation", got)
	}

	// With no matching reservation the payable error stands.
	_, err = impl.resolvePayableRejection(businessID, B2CPayoutInput{
		PhoneNumber: PhoneNumberTestConst, Amount: 10000, SupplierID: supplierID.String(),
	}, NormalizePhone(PhoneNumberTestConst), &supplierID, defaultExpenseCategory)
	if !errors.Is(err, ErrPayoutExceedsPayable) {
		t.Fatalf("error = %v, want ErrPayoutExceedsPayable", err)
	}
}
