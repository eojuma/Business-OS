package payments

import (
	"errors"
	"strings"
	"time"

	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrDuplicateReference  = errors.New("bank transaction with this reference already exists")
	ErrTransactionNotFound = errors.New("payment transaction not found")
	// ErrMpesaReceiptExists is returned when a C2B confirmation inserts a row
	// whose mpesa_receipt_number violates the partial unique index. It is a
	// duplicate, not a persistence failure.
	ErrMpesaReceiptExists = errors.New("mpesa receipt number already exists")
)

// colUpdatedAt is the shared updated_at column name used by the repository's
// compare-and-set and reconciliation updates.
const (
	colUpdatedAt            = "updated_at"
	colReconciliationStatus = "reconciliation_status"
)

type Repository interface {
	CreateMpesaTransaction(tx *MpesaTransaction) error
	GetMpesaTransactionByID(id, businessID uuid.UUID) (*MpesaTransaction, error)
	GetMpesaTransactionByIdempotency(businessID uuid.UUID, key string) (*MpesaTransaction, error)
	GetMpesaTransactionByCheckoutRequestID(checkoutReqID string) (*MpesaTransaction, error)
	GetMpesaTransactionByReceipt(receipt string) (*MpesaTransaction, error)
	// ClaimMpesaTransactionStatus atomically moves a transaction from one of the
	// allowed statuses to terminal and persists the callback metadata. It returns
	// false when another delivery already settled the transaction, which is what
	// makes callback processing safe to replay.
	ClaimMpesaTransactionStatus(tx *MpesaTransaction, terminal TransactionStatus, allowedFrom ...TransactionStatus) (bool, error)
	// ClaimMpesaReconciliation atomically moves the reconciliation status from
	// one value to another. It returns false when another request already
	// claimed the row, which serializes the manual-review settle path so one
	// receipt can never be applied twice by concurrent requests.
	ClaimMpesaReconciliation(tx *MpesaTransaction, from, to ReconciliationStatus) (bool, error)
	// ReleaseStaleReconciliationClaims returns in-progress claims older than the
	// cutoff to Unmatched so an interrupted process cannot strand an item.
	ReleaseStaleReconciliationClaims(before time.Time) error
	// DeleteMpesaTransaction removes a reservation that never reached the
	// provider (pre-dispatch failure), freeing its idempotency key for a retry.
	DeleteMpesaTransaction(id uuid.UUID) error
	// UpdateMpesaReconciliation persists reconciliation fields without touching
	// the transaction status.
	UpdateMpesaReconciliation(tx *MpesaTransaction) error
	UpdateMpesaTransaction(tx *MpesaTransaction) error
	ListMpesaTransactions(businessID uuid.UUID, limit, offset int) ([]MpesaTransaction, int64, error)

	CreateBankTransaction(tx *BankTransaction) error
	GetBankTransactionByID(id, businessID uuid.UUID) (*BankTransaction, error)
	GetBankTransactionByReference(businessID uuid.UUID, ref string) (*BankTransaction, error)
	ListBankTransactions(businessID uuid.UUID, txType string, status string, limit, offset int) ([]BankTransaction, int64, error)
	UpdateBankTransaction(tx *BankTransaction) error
	// ClaimBankReconciliation mirrors ClaimMpesaReconciliation for bank
	// transactions so the manual-review settle path is serialized there too.
	ClaimBankReconciliation(tx *BankTransaction, from, to ReconciliationStatus) (bool, error)

	ListReviewQueue(businessID uuid.UUID, source string, limit, offset int) ([]ReviewQueueItem, int64, error)
	// ListMpesaPaymentNotesByPrefix returns payment notes that start with prefix
	// across invoices, customer credit, and supplier payments for one business.
	// It is filtered in SQL so a callback or reconciliation does not load the
	// whole payment history just to check one receipt.
	ListMpesaPaymentNotesByPrefix(businessID uuid.UUID, prefix string) ([]string, error)
	FindInvoiceByNumber(businessID uuid.UUID, invoiceNumber string) (*invoices.Invoice, error)
	FindCustomerByPhone(businessID uuid.UUID, phone string) (*customers.Customer, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) CreateMpesaTransaction(tx *MpesaTransaction) error {
	err := r.db.Create(tx).Error
	if err == nil {
		return nil
	}
	if isMpesaReceiptConflict(err) {
		return ErrMpesaReceiptExists
	}
	// With TranslateError enabled the driver replaces the Postgres error with
	// the gorm.ErrDuplicatedKey sentinel, which no longer names the index. Only
	// map it when the row really is a receipt duplicate, not an idempotency-key
	// collision.
	if errors.Is(err, gorm.ErrDuplicatedKey) && r.mpesaReceiptExists(tx) {
		return ErrMpesaReceiptExists
	}
	return err
}

// mpesaReceiptExists reports whether another row already stores this receipt.
func (r *repository) mpesaReceiptExists(tx *MpesaTransaction) bool {
	receipt := strings.TrimSpace(tx.MpesaReceiptNumber)
	if receipt == "" {
		return false
	}
	var count int64
	if err := r.db.Model(&MpesaTransaction{}).
		Where("business_id = ? AND mpesa_receipt_number = ?", tx.BusinessID, receipt).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (r *repository) UpdateMpesaTransaction(tx *MpesaTransaction) error {
	// Use a targeted update instead of Save: Save writes every column, and for a
	// new row ReconciledEntityType is "" which violates the reconciled_entity_type
	// CHECK constraint (it allows NULL or the four enum values).
	return r.db.Model(&MpesaTransaction{}).Where("id = ?", tx.ID).Updates(map[string]any{
		"status":                 tx.Status,
		"result_code":            tx.ResultCode,
		"result_desc":            tx.ResultDesc,
		"merchant_request_id":    tx.MerchantRequestID,
		"checkout_request_id":    tx.CheckoutRequestID,
		"mpesa_receipt_number":   tx.MpesaReceiptNumber,
		"phone_number":           tx.PhoneNumber,
		"amount":                 tx.Amount,
		"raw_payload":            tx.RawPayload,
		"supplier_id":            tx.SupplierID,
		"expense_category":       tx.ExpenseCategory,
		colReconciliationStatus:  tx.ReconciliationStatus,
		"reconciled_entity_type": nullableEntityType(tx.ReconciledEntityType),
		"reconciled_entity_id":   tx.ReconciledEntityID,
		colUpdatedAt:             time.Now(),
	}).Error
}

// DeleteMpesaTransaction removes a reservation that never reached the provider.
func (r *repository) DeleteMpesaTransaction(id uuid.UUID) error {
	return r.db.Where("id = ?", id).Delete(&MpesaTransaction{}).Error
}

// ReleaseStaleReconciliationClaims releases interrupted in-progress claims.
func (r *repository) ReleaseStaleReconciliationClaims(before time.Time) error {
	if err := r.db.Model(&MpesaTransaction{}).
		Where("reconciliation_status = ? AND updated_at < ?", Claiming, before).
		Update(colReconciliationStatus, Unmatched).Error; err != nil {
		return err
	}
	return r.db.Model(&BankTransaction{}).
		Where("reconciliation_status = ? AND updated_at < ?", Claiming, before).
		Update(colReconciliationStatus, Unmatched).Error
}

// isMpesaReceiptConflict reports whether a create error is a violation of the
// partial unique index on mpesa_receipt_number. Gorm surfaces it as a
// gorm.ErrDuplicatedKey with the index name in the message.
func isMpesaReceiptConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "mpesa_receipt_number") ||
		strings.Contains(msg, "idx_mpesa_transactions_receipt_unique")
}

func (r *repository) GetMpesaTransactionByID(id, businessID uuid.UUID) (*MpesaTransaction, error) {
	var tx MpesaTransaction
	if err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *repository) GetMpesaTransactionByIdempotency(businessID uuid.UUID, key string) (*MpesaTransaction, error) {
	if key == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var tx MpesaTransaction
	if err := r.db.Where("business_id = ? AND idempotency_key = ?", businessID, key).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *repository) GetMpesaTransactionByCheckoutRequestID(checkoutReqID string) (*MpesaTransaction, error) {
	if checkoutReqID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var tx MpesaTransaction
	if err := r.db.Where("checkout_request_id = ?", checkoutReqID).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *repository) GetMpesaTransactionByReceipt(receipt string) (*MpesaTransaction, error) {
	if receipt == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var tx MpesaTransaction
	if err := r.db.Where("mpesa_receipt_number = ?", receipt).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

// ClaimMpesaTransactionStatus performs a compare-and-set on the transaction
// status. Only the request that wins the transition may apply the financial
// side effect, so a redelivered or concurrent callback cannot apply it twice.
func (r *repository) ClaimMpesaTransactionStatus(tx *MpesaTransaction, terminal TransactionStatus, allowedFrom ...TransactionStatus) (bool, error) {
	if len(allowedFrom) == 0 {
		allowedFrom = []TransactionStatus{StatusPending}
	}
	updates := map[string]any{
		"status":               terminal,
		"result_code":          tx.ResultCode,
		"result_desc":          tx.ResultDesc,
		"raw_payload":          tx.RawPayload,
		"mpesa_receipt_number": tx.MpesaReceiptNumber,
		"phone_number":         tx.PhoneNumber,
		"amount":               tx.Amount,
		colUpdatedAt:           time.Now(),
	}
	result := r.db.Model(&MpesaTransaction{}).
		Where("id = ? AND status IN ?", tx.ID, allowedFrom).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ClaimMpesaReconciliation performs a compare-and-set on the reconciliation
// status. Only one concurrent caller can move the row out of the expected
// status, which serializes manual reconciliation and prevents a double settle.
func (r *repository) ClaimMpesaReconciliation(tx *MpesaTransaction, from, to ReconciliationStatus) (bool, error) {
	result := r.db.Model(&MpesaTransaction{}).
		Where("id = ? AND reconciliation_status = ?", tx.ID, from).
		Updates(map[string]any{
			colReconciliationStatus: to,
			colUpdatedAt:            time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ClaimBankReconciliation is the bank-transaction counterpart of
// ClaimMpesaReconciliation.
func (r *repository) ClaimBankReconciliation(tx *BankTransaction, from, to ReconciliationStatus) (bool, error) {
	result := r.db.Model(&BankTransaction{}).
		Where("id = ? AND reconciliation_status = ?", tx.ID, from).
		Updates(map[string]any{
			colReconciliationStatus: to,
			colUpdatedAt:            time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *repository) UpdateMpesaReconciliation(tx *MpesaTransaction) error {
	updates := map[string]any{
		colReconciliationStatus:  tx.ReconciliationStatus,
		"reconciled_entity_type": nullableEntityType(tx.ReconciledEntityType),
		"reconciled_entity_id":   tx.ReconciledEntityID,
		colUpdatedAt:             time.Now(),
	}
	return r.db.Model(&MpesaTransaction{}).Where("id = ?", tx.ID).Updates(updates).Error
}

// nullableEntityType stores an unset entity type as NULL so it satisfies the
// column check constraint.
func nullableEntityType(entity ReconciledEntityType) any {
	if entity == "" {
		return nil
	}
	return entity
}

func (r *repository) ListMpesaTransactions(businessID uuid.UUID, limit, offset int) ([]MpesaTransaction, int64, error) {
	var list []MpesaTransaction
	var total int64
	q := r.db.Model(&MpesaTransaction{}).Where("business_id = ?", businessID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *repository) CreateBankTransaction(tx *BankTransaction) error {
	return r.db.Create(tx).Error
}

func (r *repository) GetBankTransactionByID(id, businessID uuid.UUID) (*BankTransaction, error) {
	var tx BankTransaction
	if err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *repository) GetBankTransactionByReference(businessID uuid.UUID, ref string) (*BankTransaction, error) {
	var tx BankTransaction
	if err := r.db.Where("business_id = ? AND reference = ?", businessID, ref).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *repository) ListBankTransactions(businessID uuid.UUID, txType string, status string, limit, offset int) ([]BankTransaction, int64, error) {
	var list []BankTransaction
	var total int64
	q := r.db.Model(&BankTransaction{}).Where("business_id = ?", businessID)
	if txType != "" {
		q = q.Where("type = ?", txType)
	}
	if status != "" {
		q = q.Where("reconciliation_status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("transacted_at DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *repository) UpdateBankTransaction(tx *BankTransaction) error {
	tx.UpdatedAt = time.Now()
	return r.db.Model(&BankTransaction{}).Where("id = ?", tx.ID).Updates(map[string]any{
		colReconciliationStatus:  tx.ReconciliationStatus,
		"reconciled_entity_type": nullableEntityType(tx.ReconciledEntityType),
		"reconciled_entity_id":   tx.ReconciledEntityID,
		colUpdatedAt:             tx.UpdatedAt,
	}).Error
}

func (r *repository) ListMpesaPaymentNotesByPrefix(businessID uuid.UUID, prefix string) ([]string, error) {
	notes := make([]string, 0)
	like := prefix + "%"

	var invoiceNotes []string
	if err := r.db.Model(&invoices.InvoicePayment{}).
		Where("business_id = ? AND note LIKE ?", businessID, like).
		Pluck("note", &invoiceNotes).Error; err != nil {
		return nil, err
	}
	notes = append(notes, invoiceNotes...)

	var customerNotes []string
	if err := r.db.Model(&customers.Payment{}).
		Where("business_id = ? AND note LIKE ?", businessID, like).
		Pluck("note", &customerNotes).Error; err != nil {
		return nil, err
	}
	notes = append(notes, customerNotes...)

	var supplierNotes []string
	if err := r.db.Model(&suppliers.Payment{}).
		Where("business_id = ? AND note LIKE ?", businessID, like).
		Pluck("note", &supplierNotes).Error; err != nil {
		return nil, err
	}
	notes = append(notes, supplierNotes...)

	return notes, nil
}

func (r *repository) ListReviewQueue(businessID uuid.UUID, source string, limit, offset int) ([]ReviewQueueItem, int64, error) {
	items := make([]ReviewQueueItem, 0)
	var total int64

	// Fetch unmatched M-Pesa transactions
	if source == "" || source == "all" || source == "mpesa" {
		var mpesaList []MpesaTransaction
		var mpesaCount int64
		mq := r.db.Model(&MpesaTransaction{}).Where("business_id = ? AND status = ? AND reconciliation_status = ?", businessID, StatusCompleted, Unmatched)
		if err := mq.Count(&mpesaCount).Error; err != nil {
			return nil, 0, err
		}
		total += mpesaCount
		if err := mq.Order("created_at DESC").Find(&mpesaList).Error; err != nil {
			return nil, 0, err
		}
		for _, m := range mpesaList {
			ref := m.MpesaReceiptNumber
			if ref == "" {
				ref = m.CheckoutRequestID
			}
			itemType := "inflow"
			if m.TransactionType == TypeB2C {
				itemType = "outflow"
			}
			items = append(items, ReviewQueueItem{
				ID:           m.ID,
				Source:       "mpesa",
				Reference:    ref,
				Counterparty: m.PhoneNumber,
				Amount:       m.Amount,
				Type:         itemType,
				Description:  m.AccountReference + " " + m.BillRefNumber,
				TransactedAt: m.CreatedAt,
				Status:       string(m.ReconciliationStatus),
			})
		}
	}

	// Fetch unmatched Bank transactions
	if source == "" || source == "all" || source == "bank" {
		var bankList []BankTransaction
		var bankCount int64
		bq := r.db.Model(&BankTransaction{}).Where("business_id = ? AND reconciliation_status = ?", businessID, Unmatched)
		if err := bq.Count(&bankCount).Error; err != nil {
			return nil, 0, err
		}
		total += bankCount
		if err := bq.Order("transacted_at DESC").Find(&bankList).Error; err != nil {
			return nil, 0, err
		}
		for _, b := range bankList {
			items = append(items, ReviewQueueItem{
				ID:           b.ID,
				Source:       "bank",
				Reference:    b.Reference,
				Counterparty: b.BankName,
				Amount:       b.Amount,
				Type:         string(b.Type),
				Description:  b.Description,
				TransactedAt: b.TransactedAt,
				Status:       string(b.ReconciliationStatus),
			})
		}
	}

	// Slice pagination in-memory for merged result
	if offset >= len(items) {
		return []ReviewQueueItem{}, total, nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total, nil
}

func (r *repository) FindInvoiceByNumber(businessID uuid.UUID, invoiceNumber string) (*invoices.Invoice, error) {
	invoiceNumber = strings.TrimSpace(invoiceNumber)
	if invoiceNumber == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var inv invoices.Invoice
	q := r.db.Where("LOWER(invoice_number) = LOWER(?)", invoiceNumber)
	if businessID != uuid.Nil {
		q = q.Where("business_id = ?", businessID)
	}
	if err := q.First(&inv).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *repository) FindCustomerByPhone(businessID uuid.UUID, phone string) (*customers.Customer, error) {
	normalized := NormalizePhone(strings.TrimSpace(phone))
	digits := onlyDigits(normalized)
	// Short suffixes match too many customers and are not safe to auto-credit.
	if len(digits) < 9 {
		return nil, gorm.ErrRecordNotFound
	}
	last9 := digits[len(digits)-9:]
	var matches []customers.Customer
	q := r.db.Where("(phone = ? OR phone LIKE ?)", normalized, "%"+last9)
	if businessID != uuid.Nil {
		q = q.Where("business_id = ?", businessID)
	}
	err := q.Limit(2).Find(&matches).Error
	if err != nil {
		return nil, err
	}
	// Only a single unambiguous match may be auto-reconciled; otherwise leave the
	// receipt for manual review instead of guessing a customer.
	if len(matches) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return &matches[0], nil
}

// onlyDigits keeps 0-9 so a phone suffix cannot act as a LIKE wildcard.
func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
