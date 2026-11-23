package payments

import (
	"time"

	"github.com/google/uuid"
)

type TransactionType string

const (
	TypeSTKPush TransactionType = "stk_push"
	TypeC2B     TransactionType = "c2b"
	TypeB2C     TransactionType = "b2c"
)

type TransactionStatus string

const (
	StatusPending   TransactionStatus = "pending"
	StatusCompleted TransactionStatus = "completed"
	StatusFailed    TransactionStatus = "failed"
	StatusCancelled TransactionStatus = "cancelled"
	// StatusTimedOut means the provider never answered in time. The payout may
	// still settle, so this is deliberately distinct from StatusFailed: a late
	// result callback is allowed to move a timed-out transaction to its final
	// state.
	StatusTimedOut TransactionStatus = "timed_out"
)

// IsTerminal reports whether a status is final and must not be overwritten by a
// redelivered callback. A timed-out transaction is not terminal.
func (s TransactionStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

type ReconciliationStatus string

const (
	Reconciled ReconciliationStatus = "reconciled"
	Unmatched  ReconciliationStatus = "unmatched"
	Ignored    ReconciliationStatus = "ignored"
	Manual     ReconciliationStatus = "manual"
)

type ReconciledEntityType string

const (
	EntityInvoice         ReconciledEntityType = "invoice"
	EntityCustomerCredit  ReconciledEntityType = "customer_credit"
	EntityExpense         ReconciledEntityType = "expense"
	EntitySupplierPayment ReconciledEntityType = "supplier_payment"
)

const (
	ResultCodeKey        = "ResultCode"
	ResultDescKey        = "ResultDesc"
	DescAccepted         = "Accepted"
	DescInvalidSignature = "Invalid signature"
	DescInvalidPayload   = "Invalid payload"
	DescInvalidJSON      = "Invalid JSON"
)

// MpesaTransaction represents a record of Lipa Na M-Pesa STK, C2B, or B2C payout.
type MpesaTransaction struct {
	ID                   uuid.UUID            `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID           uuid.UUID            `gorm:"type:uuid;index;not null" json:"business_id"`
	TransactionType      TransactionType      `gorm:"not null" json:"transaction_type"`
	MerchantRequestID    string               `gorm:"not null;default:''" json:"merchant_request_id"`
	CheckoutRequestID    string               `gorm:"index;not null;default:''" json:"checkout_request_id"`
	MpesaReceiptNumber   string               `gorm:"index;not null;default:''" json:"mpesa_receipt_number"`
	PhoneNumber          string               `gorm:"not null;default:''" json:"phone_number"`
	Amount               int64                `gorm:"not null" json:"amount"` // in cents
	Status               TransactionStatus    `gorm:"not null;default:'pending'" json:"status"`
	ResultCode           *int                 `json:"result_code,omitempty"`
	ResultDesc           string               `gorm:"not null;default:''" json:"result_desc"`
	IdempotencyKey       string               `gorm:"index" json:"idempotency_key,omitempty"`
	BillRefNumber        string               `gorm:"not null;default:''" json:"bill_ref_number"`
	AccountReference     string               `gorm:"not null;default:''" json:"account_reference"`
	ReconciliationStatus ReconciliationStatus `gorm:"not null;default:'unmatched'" json:"reconciliation_status"`
	ReconciledEntityType ReconciledEntityType `gorm:"default:null" json:"reconciled_entity_type,omitempty"`
	ReconciledEntityID   *uuid.UUID           `gorm:"type:uuid" json:"reconciled_entity_id,omitempty"`
	// SupplierID records the intended B2C payout target so the result callback
	// can settle a supplier payable instead of guessing at an expense.
	SupplierID      *uuid.UUID `gorm:"type:uuid" json:"supplier_id,omitempty"`
	ExpenseCategory string     `gorm:"not null;default:''" json:"expense_category,omitempty"`
	RawPayload      string     `gorm:"not null;default:''" json:"raw_payload,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (MpesaTransaction) TableName() string { return "mpesa_transactions" }

type BankTransactionType string

const (
	BankInflow  BankTransactionType = "inflow"
	BankOutflow BankTransactionType = "outflow"
)

// BankTransaction represents a recorded or imported bank transaction.
type BankTransaction struct {
	ID                   uuid.UUID            `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID           uuid.UUID            `gorm:"type:uuid;index;not null" json:"business_id"`
	BankName             string               `gorm:"not null;default:''" json:"bank_name"`
	Reference            string               `gorm:"not null;uniqueIndex:uq_bank_transactions_business_reference,priority:2" json:"reference"`
	Type                 BankTransactionType  `gorm:"not null" json:"type"`
	Amount               int64                `gorm:"not null" json:"amount"` // in cents
	Description          string               `gorm:"not null;default:''" json:"description"`
	TransactedAt         time.Time            `gorm:"not null" json:"transacted_at"`
	ReconciliationStatus ReconciliationStatus `gorm:"not null;default:'unmatched'" json:"reconciliation_status"`
	ReconciledEntityType ReconciledEntityType `gorm:"default:null" json:"reconciled_entity_type,omitempty"`
	ReconciledEntityID   *uuid.UUID           `gorm:"type:uuid" json:"reconciled_entity_id,omitempty"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
}

func (BankTransaction) TableName() string { return "bank_transactions" }

// Unified review queue item across M-Pesa and Bank transactions.
type ReviewQueueItem struct {
	ID           uuid.UUID `json:"id"`
	Source       string    `json:"source"` // "mpesa" or "bank"
	Reference    string    `json:"reference"`
	Counterparty string    `json:"counterparty"`
	Amount       int64     `json:"amount"` // cents
	Type         string    `json:"type"`   // "inflow" or "outflow"
	Description  string    `json:"description"`
	TransactedAt time.Time `json:"transacted_at"`
	Status       string    `json:"status"`
}

// Input DTOs
type STKPushInput struct {
	PhoneNumber      string `json:"phone_number" binding:"required"`
	Amount           int64  `json:"amount" binding:"required"` // in cents
	AccountReference string `json:"account_reference"`
	TransactionDesc  string `json:"transaction_desc"`
	IdempotencyKey   string `json:"idempotency_key"`
}

type B2CPayoutInput struct {
	PhoneNumber    string `json:"phone_number" binding:"required"`
	Amount         int64  `json:"amount" binding:"required"` // in cents
	Remarks        string `json:"remarks"`
	Occasion       string `json:"occasion"`
	IdempotencyKey string `json:"idempotency_key"`
	SupplierID     string `json:"supplier_id"`
	ExpenseCat     string `json:"expense_category"`
}

type BankManualInput struct {
	BankName     string    `json:"bank_name"`
	Reference    string    `json:"reference" binding:"required"`
	Type         string    `json:"type" binding:"required"`   // "inflow" or "outflow"
	Amount       int64     `json:"amount" binding:"required"` // in cents
	Description  string    `json:"description"`
	TransactedAt time.Time `json:"transacted_at"`
}

type StatementImportResult struct {
	TotalRows         int `json:"total_rows"`
	Imported          int `json:"imported"`
	SkippedDuplicates int `json:"skipped_duplicates"`
	Reconciled        int `json:"reconciled"`
	Unmatched         int `json:"unmatched"`
}

type ReconcileInput struct {
	Source          string `json:"source" binding:"required"`      // "mpesa" or "bank"
	TargetType      string `json:"target_type" binding:"required"` // "invoice", "customer_credit", "expense", "dismiss"
	TargetID        string `json:"target_id"`
	ExpenseCategory string `json:"expense_category"`
	Note            string `json:"note"`
}

// Safaricom Daraja DTOs
type DarajaSTKPushRequest struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	TransactionType   string `json:"TransactionType"`
	Amount            int64  `json:"Amount"` // Whole KSh for Safaricom
	PartyA            string `json:"PartyA"`
	PartyB            string `json:"PartyB"`
	PhoneNumber       string `json:"PhoneNumber"`
	CallBackURL       string `json:"CallBackURL"`
	AccountReference  string `json:"AccountReference"`
	TransactionDesc   string `json:"TransactionDesc"`
}

type DarajaSTKPushResponse struct {
	MerchantRequestID   string `json:"MerchantRequestID"`
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
	CustomerMessage     string `json:"CustomerMessage"`
}

type DarajaCallbackItem struct {
	Name  string      `json:"Name"`
	Value interface{} `json:"Value"`
}

type DarajaCallbackMetadata struct {
	Item []DarajaCallbackItem `json:"Item"`
}

type DarajaSTKCallbackBody struct {
	MerchantRequestID string                  `json:"MerchantRequestID"`
	CheckoutRequestID string                  `json:"CheckoutRequestID"`
	ResultCode        int                     `json:"ResultCode"`
	ResultDesc        string                  `json:"ResultDesc"`
	CallbackMetadata  *DarajaCallbackMetadata `json:"CallbackMetadata,omitempty"`
}

type DarajaSTKCallbackWrapper struct {
	Body struct {
		StkCallback DarajaSTKCallbackBody `json:"stkCallback"`
	} `json:"Body"`
}

type C2BValidationRequest struct {
	TransactionType   string `json:"TransactionType"`
	TransID           string `json:"TransID"`
	TransTime         string `json:"TransTime"`
	TransAmount       string `json:"TransAmount"`
	BusinessShortCode string `json:"BusinessShortCode"`
	BillRefNumber     string `json:"BillRefNumber"`
	InvoiceNumber     string `json:"InvoiceNumber"`
	MSISDN            string `json:"MSISDN"`
	FirstName         string `json:"FirstName"`
	MiddleName        string `json:"MiddleName"`
	LastName          string `json:"LastName"`
}

type C2BValidationResponse struct {
	ResultCode int    `json:"ResultCode"`
	ResultDesc string `json:"ResultDesc"`
}

type C2BConfirmationRequest struct {
	TransactionType   string `json:"TransactionType"`
	TransID           string `json:"TransID"`
	TransTime         string `json:"TransTime"`
	TransAmount       string `json:"TransAmount"`
	BusinessShortCode string `json:"BusinessShortCode"`
	BillRefNumber     string `json:"BillRefNumber"`
	InvoiceNumber     string `json:"InvoiceNumber"`
	MSISDN            string `json:"MSISDN"`
	FirstName         string `json:"FirstName"`
	MiddleName        string `json:"MiddleName"`
	LastName          string `json:"LastName"`
}

type C2BConfirmationResponse struct {
	ResultCode int    `json:"ResultCode"`
	ResultDesc string `json:"ResultDesc"`
}

type DarajaB2CRequest struct {
	InitiatorName      string `json:"InitiatorName"`
	SecurityCredential string `json:"SecurityCredential"`
	CommandID          string `json:"CommandID"`
	Amount             int64  `json:"Amount"`
	PartyA             string `json:"PartyA"`
	PartyB             string `json:"PartyB"`
	Remarks            string `json:"Remarks"`
	QueueTimeOutURL    string `json:"QueueTimeOutURL"`
	ResultURL          string `json:"ResultURL"`
	Occasion           string `json:"Occasion"`
}

type DarajaB2CResponse struct {
	ConversationID           string `json:"ConversationID"`
	OriginatorConversationID string `json:"OriginatorConversationID"`
	ResponseCode             string `json:"ResponseCode"`
	ResponseDescription      string `json:"ResponseDescription"`
}
