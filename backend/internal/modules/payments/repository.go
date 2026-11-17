package payments

import (
	"errors"
	"strings"
	"time"

	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrDuplicateReference = errors.New("bank transaction with this reference already exists")
	ErrTransactionNotFound = errors.New("payment transaction not found")
)

type Repository interface {
	CreateMpesaTransaction(tx *MpesaTransaction) error
	GetMpesaTransactionByID(id, businessID uuid.UUID) (*MpesaTransaction, error)
	GetMpesaTransactionByIdempotency(businessID uuid.UUID, key string) (*MpesaTransaction, error)
	GetMpesaTransactionByCheckoutRequestID(checkoutReqID string) (*MpesaTransaction, error)
	GetMpesaTransactionByReceipt(receipt string) (*MpesaTransaction, error)
	UpdateMpesaTransaction(tx *MpesaTransaction) error
	ListMpesaTransactions(businessID uuid.UUID, limit, offset int) ([]MpesaTransaction, int64, error)

	CreateBankTransaction(tx *BankTransaction) error
	GetBankTransactionByID(id, businessID uuid.UUID) (*BankTransaction, error)
	GetBankTransactionByReference(businessID uuid.UUID, ref string) (*BankTransaction, error)
	ListBankTransactions(businessID uuid.UUID, txType string, status string, limit, offset int) ([]BankTransaction, int64, error)
	UpdateBankTransaction(tx *BankTransaction) error

	ListReviewQueue(businessID uuid.UUID, source string, limit, offset int) ([]ReviewQueueItem, int64, error)
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
	return r.db.Create(tx).Error
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

func (r *repository) UpdateMpesaTransaction(tx *MpesaTransaction) error {
	tx.UpdatedAt = time.Now()
	return r.db.Save(tx).Error
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
	return r.db.Save(tx).Error
}

func (r *repository) ListReviewQueue(businessID uuid.UUID, source string, limit, offset int) ([]ReviewQueueItem, int64, error) {
	items := make([]ReviewQueueItem, 0)
	var total int64

	// Fetch unmatched M-Pesa transactions
	if source == "" || source == "all" || source == "mpesa" {
		var mpesaList []MpesaTransaction
		var mpesaCount int64
		mq := r.db.Model(&MpesaTransaction{}).Where("business_id = ? AND status = ? AND reconciliation_status = ?", businessID, StatusCompleted, Unmatched)
		_ = mq.Count(&mpesaCount)
		total += mpesaCount
		if err := mq.Order("created_at DESC").Find(&mpesaList).Error; err == nil {
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
	}

	// Fetch unmatched Bank transactions
	if source == "" || source == "all" || source == "bank" {
		var bankList []BankTransaction
		var bankCount int64
		bq := r.db.Model(&BankTransaction{}).Where("business_id = ? AND reconciliation_status = ?", businessID, Unmatched)
		_ = bq.Count(&bankCount)
		total += bankCount
		if err := bq.Order("transacted_at DESC").Find(&bankList).Error; err == nil {
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
	if err := r.db.Where("business_id = ? AND LOWER(invoice_number) = LOWER(?)", businessID, invoiceNumber).First(&inv).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *repository) FindCustomerByPhone(businessID uuid.UUID, phone string) (*customers.Customer, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil, gorm.ErrRecordNotFound
	}
	normalized := NormalizePhone(phone)
	last9 := normalized
	if len(last9) > 9 {
		last9 = last9[len(last9)-9:]
	}
	var cust customers.Customer
	if err := r.db.Where("business_id = ? AND (phone = ? OR phone LIKE ?)", businessID, normalized, "%"+last9).First(&cust).Error; err != nil {
		return nil, err
	}
	return &cust, nil
}
