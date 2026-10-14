package receipts

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// Errors returned by the receipts service.
var (
	ErrEmptyImage        = errors.New("an image is required")
	ErrUnsupportedImage  = errors.New("the image must be a JPEG, PNG or WebP file")
	ErrImageTooLarge     = errors.New("the image is larger than 10 MB")
	ErrNoLineItems       = errors.New("a receipt must have at least one line item")
	ErrInvalidRecordType = errors.New("record_type must be purchase or sale")
	ErrMissingSupplier   = errors.New("a purchase requires a supplier")
	ErrMissingProduct    = errors.New("every line item requires a product")
	ErrInvalidLineItem   = errors.New("line item quantities must be positive and prices cannot be negative")
	ErrProductNotOwned   = errors.New("one or more products do not belong to this business")
	ErrSupplierNotFound  = errors.New("the selected supplier does not belong to this business")
	ErrCustomerNotFound  = errors.New("the selected customer does not belong to this business")
)

// MaxImageBytes caps an upload before it reaches the OCR provider.
const MaxImageBytes = 10 << 20

// Record types the receipts module can commit to.
const (
	RecordTypePurchase = "purchase"
	RecordTypeSale     = "sale"
)

// Image content types accepted by the vision model.
const (
	contentTypeJPEG = "image/jpeg"
	contentTypePNG  = "image/png"
	contentTypeWebP = "image/webp"
)

// supportedImageTypes is the allowlist of formats the vision model accepts.
var supportedImageTypes = map[string]bool{
	contentTypeJPEG: true,
	contentTypePNG:  true,
	contentTypeWebP: true,
}

// ProductInfo is the narrow view of a product the receipts module needs.
type ProductInfo struct {
	ID   uuid.UUID
	Name string
}

// ProductLister is the read-only view of a business's products.
type ProductLister interface {
	List(businessID uuid.UUID) ([]ProductInfo, error)
}

// SupplierReader checks that a supplier belongs to the business.
type SupplierReader interface {
	Exists(businessID, supplierID uuid.UUID) (bool, error)
}

// CustomerReader checks that a customer belongs to the business.
type CustomerReader interface {
	Exists(businessID, customerID uuid.UUID) (bool, error)
}

// ScannedLineItem is one extracted line returned to the owner, with a
// best-effort match against an existing product so the review screen can
// pre-select it.
type ScannedLineItem struct {
	Description        string     `json:"description"`
	Quantity           int64      `json:"quantity"`
	UnitPrice          int64      `json:"unit_price"`
	Subtotal           int64      `json:"subtotal"`
	MatchedProductID   *uuid.UUID `json:"matched_product_id,omitempty"`
	MatchedProductName string     `json:"matched_product_name,omitempty"`
}

// ScanResult is the read-only output of a scan. Nothing is written until the
// owner confirms and calls Commit.
type ScanResult struct {
	Merchant   string            `json:"merchant"`
	Date       string            `json:"date"`
	Currency   string            `json:"currency"`
	LineItems  []ScannedLineItem `json:"line_items"`
	Total      int64             `json:"total"`
	Confidence string            `json:"confidence"`
	Notes      string            `json:"notes"`
}

// CommitLineItem is a reviewed and corrected line the owner is ready to save.
type CommitLineItem struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int64     `json:"quantity"`
	UnitPrice int64     `json:"unit_price"`
}

// CommitInput is the explicit confirmation payload. It is the only way an OCR
// result becomes a record.
type CommitInput struct {
	RecordType string           `json:"record_type"`
	SupplierID *uuid.UUID       `json:"supplier_id"`
	CustomerID *uuid.UUID       `json:"customer_id"`
	SaleType   string           `json:"sale_type"`
	Note       string           `json:"note"`
	AmountPaid int64            `json:"amount_paid"`
	LineItems  []CommitLineItem `json:"line_items"`
}

// CommitResult is the record that was created.
type CommitResult struct {
	RecordType  string    `json:"record_type"`
	ID          uuid.UUID `json:"id"`
	TotalAmount int64     `json:"total_amount"`
}

// PurchaseInput is the narrow input the receipts module sends to the purchases
// module. UnitPrice carries the reviewed unit cost.
type PurchaseInput struct {
	BusinessID uuid.UUID
	SupplierID uuid.UUID
	AmountPaid int64
	Note       string
	Items      []CommitLineItem
}

// PurchaseCommitResult is the purchase created on commit.
type PurchaseCommitResult struct {
	ID          uuid.UUID
	TotalAmount int64
}

// PurchaseCreator is satisfied by an adapter over purchases.Service, so this
// module never imports the purchases package.
type PurchaseCreator interface {
	CreatePurchase(input PurchaseInput) (*PurchaseCommitResult, error)
}

// SaleInput is the narrow input the receipts module sends to the sales module.
// Sale pricing comes from the catalogue, so UnitPrice is intentionally absent.
type SaleInput struct {
	BusinessID uuid.UUID
	CustomerID *uuid.UUID
	SaleType   string
	Note       string
	Items      []CommitLineItem
}

// SaleCommitResult is the sale created on commit.
type SaleCommitResult struct {
	ID          uuid.UUID
	TotalAmount int64
}

// SaleCreator is satisfied by an adapter over sales.Service.
type SaleCreator interface {
	CreateSale(input SaleInput) (*SaleCommitResult, error)
}

// Service scans receipt images and commits reviewed results.
type Service interface {
	Scan(ctx context.Context, businessID uuid.UUID, image []byte, contentType string) (*ScanResult, error)
	Commit(businessID uuid.UUID, input CommitInput) (*CommitResult, error)
}

type service struct {
	ocr       OCRClient
	products  ProductLister
	suppliers SupplierReader
	customers CustomerReader
	purchases PurchaseCreator
	sales     SaleCreator
}

// NewService wires the OCR client and the narrow readers/creators the receipts
// module needs from other modules.
func NewService(ocr OCRClient, products ProductLister, suppliers SupplierReader, customers CustomerReader, purchases PurchaseCreator, sales SaleCreator) Service {
	return &service{
		ocr:       ocr,
		products:  products,
		suppliers: suppliers,
		customers: customers,
		purchases: purchases,
		sales:     sales,
	}
}

// Scan extracts line items from an image. It never writes: the owner must
// review the result and call Commit explicitly.
func (s *service) Scan(ctx context.Context, businessID uuid.UUID, image []byte, contentType string) (*ScanResult, error) {
	if err := validateImage(image, contentType); err != nil {
		return nil, err
	}

	extraction, err := s.ocr.Extract(ctx, image, contentType)
	if err != nil {
		return nil, err
	}

	products, err := s.products.List(businessID)
	if err != nil {
		return nil, err
	}

	lines := normalizeLineItems(extraction.LineItems)
	attachProductMatches(lines, products)

	total := int64(extraction.Total)
	if total == 0 {
		for _, line := range lines {
			total += line.Subtotal
		}
	}

	return &ScanResult{
		Merchant:   strings.TrimSpace(extraction.Merchant),
		Date:       strings.TrimSpace(extraction.Date),
		Currency:   strings.TrimSpace(extraction.Currency),
		LineItems:  lines,
		Total:      total,
		Confidence: strings.TrimSpace(extraction.Confidence),
		Notes:      strings.TrimSpace(extraction.Notes),
	}, nil
}

// Commit is the explicit confirmation step. It validates the reviewed data and
// delegates the actual write to the purchases or sales module.
func (s *service) Commit(businessID uuid.UUID, input CommitInput) (*CommitResult, error) {
	recordType := strings.ToLower(strings.TrimSpace(input.RecordType))
	if recordType != RecordTypePurchase && recordType != RecordTypeSale {
		return nil, ErrInvalidRecordType
	}
	if len(input.LineItems) == 0 {
		return nil, ErrNoLineItems
	}
	for _, item := range input.LineItems {
		if item.ProductID == uuid.Nil {
			return nil, ErrMissingProduct
		}
		if item.Quantity <= 0 || item.UnitPrice < 0 {
			return nil, ErrInvalidLineItem
		}
	}

	// Never trust IDs from the request: every referenced product must belong
	// to the authenticated business, so one store cannot write a record that
	// points at another store's data.
	products, err := s.products.List(businessID)
	if err != nil {
		return nil, err
	}
	owned := make(map[uuid.UUID]struct{}, len(products))
	for _, product := range products {
		owned[product.ID] = struct{}{}
	}
	for _, item := range input.LineItems {
		if _, ok := owned[item.ProductID]; !ok {
			return nil, ErrProductNotOwned
		}
	}

	if recordType == RecordTypePurchase {
		if input.SupplierID == nil || *input.SupplierID == uuid.Nil {
			return nil, ErrMissingSupplier
		}
		ok, err := s.suppliers.Exists(businessID, *input.SupplierID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrSupplierNotFound
		}
		result, err := s.purchases.CreatePurchase(PurchaseInput{
			BusinessID: businessID,
			SupplierID: *input.SupplierID,
			AmountPaid: input.AmountPaid,
			Note:       input.Note,
			Items:      input.LineItems,
		})
		if err != nil {
			return nil, err
		}
		return &CommitResult{RecordType: RecordTypePurchase, ID: result.ID, TotalAmount: result.TotalAmount}, nil
	}

	if input.CustomerID != nil && *input.CustomerID != uuid.Nil {
		ok, err := s.customers.Exists(businessID, *input.CustomerID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCustomerNotFound
		}
	}

	saleType := strings.ToLower(strings.TrimSpace(input.SaleType))
	if saleType == "" {
		saleType = "cash"
	}
	result, err := s.sales.CreateSale(SaleInput{
		BusinessID: businessID,
		CustomerID: input.CustomerID,
		SaleType:   saleType,
		Note:       input.Note,
		Items:      input.LineItems,
	})
	if err != nil {
		return nil, err
	}
	return &CommitResult{RecordType: RecordTypeSale, ID: result.ID, TotalAmount: result.TotalAmount}, nil
}

func validateImage(image []byte, contentType string) error {
	if len(image) == 0 {
		return ErrEmptyImage
	}
	if int64(len(image)) > MaxImageBytes {
		return ErrImageTooLarge
	}
	if !supportedImageTypes[contentType] {
		return ErrUnsupportedImage
	}
	return nil
}

func normalizeLineItems(items []ExtractedLineItem) []ScannedLineItem {
	lines := make([]ScannedLineItem, 0, len(items))
	for _, item := range items {
		quantity := int64(item.Quantity)
		if quantity <= 0 {
			quantity = 1
		}
		unitPrice := int64(item.UnitPrice)
		subtotal := int64(item.Subtotal)
		if subtotal == 0 && unitPrice > 0 {
			subtotal = unitPrice * quantity
		}
		lines = append(lines, ScannedLineItem{
			Description: strings.TrimSpace(item.Description),
			Quantity:    quantity,
			UnitPrice:   unitPrice,
			Subtotal:    subtotal,
		})
	}
	return lines
}

func attachProductMatches(lines []ScannedLineItem, products []ProductInfo) {
	for i := range lines {
		match := bestProductMatch(products, lines[i].Description)
		if match == nil {
			continue
		}
		id := match.ID
		lines[i].MatchedProductID = &id
		lines[i].MatchedProductName = match.Name
	}
}

var matchStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "and": true, "x": true,
	"bag": true, "bags": true, "box": true, "boxes": true, "piece": true,
	"pieces": true, "pcs": true, "unit": true, "units": true,
}

// bestProductMatch finds the most likely existing product for a scanned
// description. It is best-effort only: the owner confirms the final choice.
func bestProductMatch(products []ProductInfo, description string) *ProductInfo {
	query := strings.ToLower(strings.TrimSpace(description))
	if query == "" {
		return nil
	}

	// Prefer a direct substring match on the whole description.
	for i := range products {
		if strings.Contains(strings.ToLower(products[i].Name), query) {
			return &products[i]
		}
	}

	tokens := significantTokens(query)
	if len(tokens) == 0 {
		return nil
	}

	bestScore := 0
	var best *ProductInfo
	for i := range products {
		name := strings.ToLower(products[i].Name)
		score := 0
		for _, token := range tokens {
			if strings.Contains(name, token) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = &products[i]
		}
	}
	if bestScore == 0 {
		return nil
	}
	return best
}

func significantTokens(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '?' || r == '!' || r == '\'' || r == '"' || r == '-'
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) < 2 || matchStopwords[field] {
			continue
		}
		tokens = append(tokens, field)
	}
	return tokens
}
