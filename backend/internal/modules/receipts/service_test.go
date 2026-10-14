package receipts

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeOCR struct {
	extraction *Extraction
	err        error
}

func (f *fakeOCR) Extract(context.Context, []byte, string) (*Extraction, error) {
	return f.extraction, f.err
}

type fakeProducts struct{ items []ProductInfo }

func (f *fakeProducts) List(uuid.UUID) ([]ProductInfo, error) { return f.items, nil }

type fakePurchases struct {
	calls int
	input PurchaseInput
}

func (f *fakePurchases) CreatePurchase(input PurchaseInput) (*PurchaseCommitResult, error) {
	f.calls++
	f.input = input
	return &PurchaseCommitResult{ID: uuid.New(), TotalAmount: 12345}, nil
}

type fakeSales struct {
	calls int
	input SaleInput
}

func (f *fakeSales) CreateSale(input SaleInput) (*SaleCommitResult, error) {
	f.calls++
	f.input = input
	return &SaleCommitResult{ID: uuid.New(), TotalAmount: 9900}, nil
}

func newTestService(ocr OCRClient, products ProductLister) (Service, *fakePurchases, *fakeSales) {
	purchases := &fakePurchases{}
	sales := &fakeSales{}
	return NewService(ocr, products, purchases, sales), purchases, sales
}

func TestScanReturnsLineItemsWithoutWriting(t *testing.T) {
	ocr := &fakeOCR{extraction: &Extraction{
		Merchant: "Nakumatt Hardware",
		Currency: "KSh",
		LineItems: []ExtractedLineItem{
			{Description: "Cement 50kg", Quantity: 3, UnitPrice: 45050, Subtotal: 135150},
			{Description: "Nails 1kg", Quantity: 2, UnitPrice: 20000},
		},
		Total:      175150,
		Confidence: "high",
	}}
	svc, purchases, sales := newTestService(ocr, &fakeProducts{})

	result, err := svc.Scan(context.Background(), uuid.New(), []byte("jpeg-bytes"), contentTypeJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.LineItems) != 2 {
		t.Fatalf("got %d line items, want 2", len(result.LineItems))
	}
	if result.Total != 175150 {
		t.Fatalf("got total %d, want 175150", result.Total)
	}
	if result.LineItems[1].Subtotal != 40000 {
		t.Fatalf("got subtotal %d, want 40000 (unit price * quantity)", result.LineItems[1].Subtotal)
	}
	if purchases.calls != 0 || sales.calls != 0 {
		t.Fatalf("Scan wrote to the database (purchases=%d sales=%d); writes must wait for Commit", purchases.calls, sales.calls)
	}
}

func TestScanFallsBackToSubtotalSum(t *testing.T) {
	ocr := &fakeOCR{extraction: &Extraction{
		LineItems: []ExtractedLineItem{
			{Description: "Hammer", Quantity: 1, Subtotal: 12000},
			{Description: "Saw", Quantity: 1, Subtotal: 34000},
		},
	}}
	svc, _, _ := newTestService(ocr, &fakeProducts{})

	result, err := svc.Scan(context.Background(), uuid.New(), []byte("img"), contentTypePNG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 46000 {
		t.Fatalf("got total %d, want 46000 summed from subtotals", result.Total)
	}
}

func TestScanMatchesExistingProduct(t *testing.T) {
	cementID := uuid.New()
	ocr := &fakeOCR{extraction: &Extraction{
		LineItems: []ExtractedLineItem{{Description: "Cement 50kg bag", Quantity: 1}},
	}}
	svc, _, _ := newTestService(ocr, &fakeProducts{items: []ProductInfo{{ID: cementID, Name: "Cement 50kg"}}})

	result, err := svc.Scan(context.Background(), uuid.New(), []byte("img"), contentTypeJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LineItems[0].MatchedProductID == nil || *result.LineItems[0].MatchedProductID != cementID {
		t.Fatalf("expected the cement product to be matched, got %+v", result.LineItems[0])
	}
}

func TestScanRejectsUnsupportedImage(t *testing.T) {
	svc, _, _ := newTestService(&fakeOCR{}, &fakeProducts{})

	if _, err := svc.Scan(context.Background(), uuid.New(), []byte("pdf"), "application/pdf"); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("got err %v, want ErrUnsupportedImage", err)
	}
	if _, err := svc.Scan(context.Background(), uuid.New(), nil, contentTypeJPEG); !errors.Is(err, ErrEmptyImage) {
		t.Fatalf("got err %v, want ErrEmptyImage", err)
	}
}

func TestCommitPurchaseDelegates(t *testing.T) {
	svc, purchases, sales := newTestService(&fakeOCR{}, &fakeProducts{})
	supplierID := uuid.New()
	productID := uuid.New()

	result, err := svc.Commit(uuid.New(), CommitInput{
		RecordType: RecordTypePurchase,
		SupplierID: &supplierID,
		LineItems:  []CommitLineItem{{ProductID: productID, Quantity: 5, UnitPrice: 45050}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if purchases.calls != 1 || sales.calls != 0 {
		t.Fatalf("got purchases=%d sales=%d, want the purchase to be created", purchases.calls, sales.calls)
	}
	if purchases.input.SupplierID != supplierID || purchases.input.Items[0].UnitPrice != 45050 {
		t.Fatalf("purchase input not translated correctly: %+v", purchases.input)
	}
	if result.RecordType != RecordTypePurchase {
		t.Fatalf("got record type %q, want purchase", result.RecordType)
	}
}

func TestCommitSaleDelegates(t *testing.T) {
	svc, purchases, sales := newTestService(&fakeOCR{}, &fakeProducts{})

	result, err := svc.Commit(uuid.New(), CommitInput{
		RecordType: RecordTypeSale,
		LineItems:  []CommitLineItem{{ProductID: uuid.New(), Quantity: 1, UnitPrice: 100}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sales.calls != 1 || purchases.calls != 0 {
		t.Fatalf("got purchases=%d sales=%d, want the sale to be created", purchases.calls, sales.calls)
	}
	if sales.input.SaleType != "cash" {
		t.Fatalf("got sale type %q, want the cash default", sales.input.SaleType)
	}
	if result.RecordType != RecordTypeSale {
		t.Fatalf("got record type %q, want sale", result.RecordType)
	}
}

func TestCommitRejectsInvalidInput(t *testing.T) {
	svc, _, _ := newTestService(&fakeOCR{}, &fakeProducts{})
	productID := uuid.New()

	cases := []struct {
		name  string
		input CommitInput
		want  error
	}{
		{
			name:  "bad record type",
			input: CommitInput{RecordType: "expense", LineItems: []CommitLineItem{{ProductID: productID, Quantity: 1}}},
			want:  ErrInvalidRecordType,
		},
		{
			name:  "no line items",
			input: CommitInput{RecordType: RecordTypeSale},
			want:  ErrNoLineItems,
		},
		{
			name:  "purchase without supplier",
			input: CommitInput{RecordType: RecordTypePurchase, LineItems: []CommitLineItem{{ProductID: productID, Quantity: 1}}},
			want:  ErrMissingSupplier,
		},
		{
			name:  "line without product",
			input: CommitInput{RecordType: RecordTypeSale, LineItems: []CommitLineItem{{Quantity: 1}}},
			want:  ErrMissingProduct,
		},
		{
			name:  "non-positive quantity",
			input: CommitInput{RecordType: RecordTypeSale, LineItems: []CommitLineItem{{ProductID: productID, Quantity: 0}}},
			want:  ErrInvalidLineItem,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Commit(uuid.New(), tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("got err %v, want %v", err, tc.want)
			}
		})
	}
}
