package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// In-memory fake repository for payments testing
type fakePaymentRepository struct {
	mpesaTxs          map[string]*MpesaTransaction
	bankTxs           map[string]*BankTransaction
	invoices          map[string]*invoices.Invoice
	custs             map[string]*customers.Customer
	paymentNotes      []string
	claimErr          error
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
			return tx, nil
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

func (f *fakePaymentRepository) UpdateMpesaReconciliation(tx *MpesaTransaction) error {
	if f.reconciliationErr != nil {
		return f.reconciliationErr
	}
	stored, ok := f.mpesaTxs[tx.ID.String()]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	stored.ReconciliationStatus = tx.ReconciliationStatus
	stored.ReconciledEntityType = tx.ReconciledEntityType
	stored.ReconciledEntityID = tx.ReconciledEntityID
	return nil
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

func (f *fakePaymentRepository) UpdateBankTransaction(tx *BankTransaction) error {
	f.bankTxs[tx.ID.String()] = tx
	return nil
}

func (f *fakePaymentRepository) ListReviewQueue(businessID uuid.UUID, source string, limit, offset int) ([]ReviewQueueItem, int64, error) {
	items := make([]ReviewQueueItem, 0)
	if source == "" || source == "all" || source == "mpesa" {
		for _, m := range f.mpesaTxs {
			if m.BusinessID == businessID && m.ReconciliationStatus == Unmatched && m.Status == StatusCompleted {
				items = append(items, ReviewQueueItem{
					ID:        m.ID,
					Source:    "mpesa",
					Reference: m.MpesaReceiptNumber,
					Amount:    m.Amount,
					Type:      "inflow",
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
	for _, inv := range f.invoices {
		if (businessID == uuid.Nil || inv.BusinessID == businessID) && strings.EqualFold(inv.InvoiceNumber, invoiceNumber) {
			return inv, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
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

// ListAllMpesaPaymentNotes returns every payment note stored across invoices,
// customer credit, and supplier payments for one business. The fake keeps a
// flat slice so the receipt-already-applied check can scan it.
func (f *fakePaymentRepository) ListAllMpesaPaymentNotes(businessID uuid.UUID) ([]string, error) {
	return f.paymentNotes, nil
}

// Mock MpesaClient
type mockMpesaClient struct {
	stkResp    *DarajaSTKPushResponse
	stkErr     error
	b2cResp    *DarajaB2CResponse
	b2cErr     error
	lastReq    DarajaSTKPushRequest
	lastB2CReq DarajaB2CRequest
}

func (m *mockMpesaClient) InitiateSTKPush(ctx context.Context, req DarajaSTKPushRequest) (*DarajaSTKPushResponse, error) {
	m.lastReq = req
	return m.stkResp, m.stkErr
}

func (m *mockMpesaClient) InitiateB2CPayout(ctx context.Context, req DarajaB2CRequest) (*DarajaB2CResponse, error) {
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
	recordErr      error
	payments       []suppliers.Payment
}

func (m *mockSuppliersService) Create(suppliers.CreateInput) (*suppliers.Supplier, error) {
	return nil, nil
}
func (m *mockSuppliersService) Get(id, businessID uuid.UUID) (*suppliers.Supplier, error) {
	if m.paidSupplierID == id {
		return &suppliers.Supplier{ID: id, BusinessID: businessID}, nil
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
		PhoneNumber:      "0712345678",
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
	vRes, err := svc.HandleC2BValidation("c2b-secret", vReq)
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
	cRes, err := svc.HandleC2BConfirmation("c2b-secret", cReq, uuid.Nil)
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
	if tx.ReconciliationStatus != Reconciled || tx.ReconciledEntityType != EntityCustomerCredit {
		t.Fatalf("expected reconciled to customer credit, got %s / %s", tx.ReconciliationStatus, tx.ReconciledEntityType)
	}
	if custSvc.paidCustID != custID || custSvc.paidAmount != 25000 {
		t.Fatalf("customer payment not credited: id=%s amount=%d", custSvc.paidCustID, custSvc.paidAmount)
	}

	// Duplicate TransID handling: idempotent
	cResDup, err := svc.HandleC2BConfirmation("c2b-secret", cReq, uuid.Nil)
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
		BankName:     "Equity Bank",
		Reference:    "EQ-987654",
		Type:         "outflow",
		Amount:       150000,
		Description:  "Office supplies payment",
		TransactedAt: time.Now(),
	}

	tx, err := svc.RecordManualBankTransfer(businessID, input)
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
	_, err = svc.RecordManualBankTransfer(businessID, input)
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
		Source:     "mpesa",
		TargetType: "invoice",
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
	if err := svc.HandleSTKCallback(computeHMAC(cfg.MpesaCallbackSecret, body), body); err != nil {
		t.Fatalf("callback should be acknowledged after the status is durable: %v", err)
	}
	stored, _ := repo.GetMpesaTransactionByID(tx.ID, businessID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", stored.Status)
	}
	if stored.ReconciliationStatus != Unmatched {
		t.Fatalf("reconciliation = %s, want unmatched for the review queue", stored.ReconciliationStatus)
	}
}

// Test 9: callback URLs embed the configured secret when no explicit URL is set.
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
		MpesaPasskey:        "passkey",
	}
	svc := NewService(repo, client, cfg, nil, nil, nil, nil)

	if _, err := svc.InitiateSTKPush(context.Background(), uuid.New(), STKPushInput{
		PhoneNumber: "0712345678",
		Amount:      1000,
	}); err != nil {
		t.Fatalf("InitiateSTKPush failed: %v", err)
	}
	if got, want := client.lastReq.CallBackURL, "https://api.example.com/api/v1/payments/mpesa/callback/stk?token=callback-token"; got != want {
		t.Fatalf("STK callback URL = %q, want %q", got, want)
	}

	if _, err := svc.InitiateB2C(context.Background(), uuid.New(), B2CPayoutInput{
		PhoneNumber: "0712345678",
		Amount:      1000,
	}); err != nil {
		t.Fatalf("InitiateB2C failed: %v", err)
	}
	if got, want := client.lastB2CReq.ResultURL, "https://api.example.com/api/v1/payments/mpesa/callback/b2c-result?token=callback-token"; got != want {
		t.Fatalf("B2C result URL = %q, want %q", got, want)
	}
	if got, want := client.lastB2CReq.QueueTimeOutURL, "https://api.example.com/api/v1/payments/mpesa/callback/b2c-timeout?token=callback-token"; got != want {
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

	tx, err := svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber: "0712345678",
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
	tx := &MpesaTransaction{
		ID:                   uuid.New(),
		BusinessID:           businessID,
		TransactionType:      TypeB2C,
		CheckoutRequestID:    "CONV-DONE-1",
		Amount:               5000,
		Status:               StatusCompleted,
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
		Source: "bank", TargetType: "invoice", TargetID: uuid.New().String(),
	}); !errors.Is(err, ErrTargetDirectionMismatch) {
		t.Fatalf("outflow -> invoice error = %v, want ErrTargetDirectionMismatch", err)
	}
	if err := svc.ReconcileReviewItem(businessID, inflow.ID, ReconcileInput{
		Source: "bank", TargetType: "expense",
	}); !errors.Is(err, ErrTargetDirectionMismatch) {
		t.Fatalf("inflow -> expense error = %v, want ErrTargetDirectionMismatch", err)
	}

	if err := svc.ReconcileReviewItem(businessID, outflow.ID, ReconcileInput{
		Source: "bank", TargetType: "expense", ExpenseCategory: "Transport",
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
		Source: "mpesa", TargetType: "invoice", TargetID: uuid.New().String(),
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
	sig := "any-signature"
	if ValidateCallbackSignature("", body, sig) {
		t.Fatal("empty secret must reject signature")
	}
	if ValidateCallbackSignature("   ", body, sig) {
		t.Fatal("whitespace secret must reject signature")
	}
}

func TestIdempotencyKeyConflictDetection(t *testing.T) {
	repo := newFakePaymentRepository()
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)
	businessID := uuid.New()

	// Initial STK push
	stkTx := &MpesaTransaction{
		ID:              uuid.New(),
		BusinessID:      businessID,
		TransactionType: TypeSTKPush,
		PhoneNumber:     "254712345678",
		Amount:          5000,
		IdempotencyKey:  "shared-key-1",
		Status:          StatusPending,
	}
	_ = repo.CreateMpesaTransaction(stkTx)

	// Replay with identical params returns the same transaction
	identical, err := svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber:    "0712345678",
		Amount:         5000,
		IdempotencyKey: "shared-key-1",
	})
	if err != nil || identical == nil || identical.ID != stkTx.ID {
		t.Fatalf("identical replay failed: %v", err)
	}

	// Replay with different amount fails with conflict
	_, err = svc.InitiateSTKPush(context.Background(), businessID, STKPushInput{
		PhoneNumber:    "0712345678",
		Amount:         6000,
		IdempotencyKey: "shared-key-1",
	})
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict, got %v", err)
	}

	// Replay for B2C with the STK key fails with conflict
	_, err = svc.InitiateB2C(context.Background(), businessID, B2CPayoutInput{
		PhoneNumber:    "0712345678",
		Amount:         5000,
		IdempotencyKey: "shared-key-1",
	})
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict for cross-type replay, got %v", err)
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
	cfg := &config.Config{MpesaCallbackSecret: "sec"}
	svc := NewService(repo, nil, cfg, nil, nil, nil, nil)

	sig := computeHMAC(cfg.MpesaCallbackSecret, nil)
	resp, err := svc.HandleC2BConfirmation(sig, C2BConfirmationRequest{
		TransID:     "   ",
		TransAmount: "100.00",
		MSISDN:      "254712345678",
	}, uuid.Nil)
	if err == nil || resp.ResultCode == 0 {
		t.Fatal("expected error for empty TransID")
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
	})
	if err == nil {
		t.Fatal("expected error for invalid bank direction")
	}

	// Inflow and withdrawal recognized
	inflowTx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference: "REF-INFLOW",
		Amount:    1000,
		Type:      "deposit",
	})
	if err != nil || inflowTx.Type != BankInflow {
		t.Fatalf("expected deposit to normalize to BankInflow: %v", err)
	}

	outflowTx, err := svc.RecordManualBankTransfer(businessID, BankManualInput{
		Reference: "REF-OUTFLOW",
		Amount:    1000,
		Type:      "withdrawal",
	})
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
