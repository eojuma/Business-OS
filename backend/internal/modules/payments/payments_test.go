package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/finance"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// In-memory fake repository for payments testing
type fakePaymentRepository struct {
	mpesaTxs map[string]*MpesaTransaction
	bankTxs  map[string]*BankTransaction
	invoices map[string]*invoices.Invoice
	custs    map[string]*customers.Customer
}

func newFakePaymentRepository() *fakePaymentRepository {
	return &fakePaymentRepository{
		mpesaTxs: make(map[string]*MpesaTransaction),
		bankTxs:  make(map[string]*BankTransaction),
		invoices: make(map[string]*invoices.Invoice),
		custs:    make(map[string]*customers.Customer),
	}
}

func (f *fakePaymentRepository) CreateMpesaTransaction(tx *MpesaTransaction) error {
	if tx.ID == uuid.Nil {
		tx.ID = uuid.New()
	}
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

func (f *fakePaymentRepository) UpdateMpesaTransaction(tx *MpesaTransaction) error {
	f.mpesaTxs[tx.ID.String()] = tx
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

// Mock MpesaClient
type mockMpesaClient struct {
	stkResp *DarajaSTKPushResponse
	stkErr  error
	b2cResp *DarajaB2CResponse
	b2cErr  error
	lastReq DarajaSTKPushRequest
}

func (m *mockMpesaClient) InitiateSTKPush(ctx context.Context, req DarajaSTKPushRequest) (*DarajaSTKPushResponse, error) {
	m.lastReq = req
	return m.stkResp, m.stkErr
}

func (m *mockMpesaClient) InitiateB2CPayout(ctx context.Context, req DarajaB2CRequest) (*DarajaB2CResponse, error) {
	return m.b2cResp, m.b2cErr
}

// Mock invoices service
type mockInvoicesService struct {
	paidInvoiceID uuid.UUID
	paidAmount    int64
	recordErr     error
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
	return &invoices.Invoice{ID: id, BusinessID: businessID, AmountPaid: input.Amount, Status: invoices.StatusPaid}, nil
}

func (m *mockInvoicesService) ListPayments(id, businessID uuid.UUID) ([]invoices.InvoicePayment, error) {
	return nil, nil
}

// Mock customers service
type mockCustomersService struct {
	paidCustID uuid.UUID
	paidAmount int64
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
	return &customers.Customer{ID: id, BusinessID: businessID}, nil
}
func (m *mockCustomersService) ListPayments(uuid.UUID, uuid.UUID) ([]customers.Payment, error) {
	return nil, nil
}
func (m *mockCustomersService) FindOrCreateByPhone(uuid.UUID, string, string, string) (*customers.Customer, error) {
	return nil, nil
}
func (m *mockCustomersService) ChargeCreditTx(*gorm.DB, uuid.UUID, uuid.UUID, int64) error {
	return nil
}

// Mock finance service
type mockFinanceService struct {
	createdExpense *finance.Expense
}

func (m *mockFinanceService) CreateExpense(in finance.CreateExpenseInput) (*finance.Expense, error) {
	exp := &finance.Expense{
		ID:         uuid.New(),
		BusinessID: in.BusinessID,
		Category:   in.Category,
		Amount:     in.Amount,
	}
	m.createdExpense = exp
	return exp, nil
}
func (m *mockFinanceService) ListExpenses(uuid.UUID, time.Time, time.Time) ([]finance.Expense, error) {
	return nil, nil
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
	svc := NewService(repo, client, cfg, nil, nil, nil)

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
	svc := NewService(repo, nil, cfg, invSvc, nil, nil)

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
	svc := NewService(repo, nil, cfg, nil, custSvc, nil)

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
	cRes, err := svc.HandleC2BConfirmation("c2b-secret", cReq)
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
	cResDup, err := svc.HandleC2BConfirmation("c2b-secret", cReq)
	if err != nil || cResDup.ResultCode != 0 {
		t.Fatalf("duplicate confirmation failed: %v", err)
	}
}

// Test 4: Bank transfer manual entry and duplicate rejection
func TestBankTransferManualEntry(t *testing.T) {
	repo := newFakePaymentRepository()
	finSvc := &mockFinanceService{}
	svc := NewService(repo, nil, &config.Config{}, nil, nil, finSvc)

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
	svc := NewService(repo, nil, &config.Config{}, invSvc, nil, nil)

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
	svc := NewService(repo, nil, &config.Config{}, invSvc, nil, finSvc)

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
