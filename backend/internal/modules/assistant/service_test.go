package assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeAI struct {
	reply string
	err   error
}

func (f *fakeAI) Complete(string) (string, error) { return f.reply, f.err }

type fakeProducts struct{ items []ProductInfo }

func (f *fakeProducts) List(uuid.UUID) ([]ProductInfo, error) { return f.items, nil }

type fakeSales struct{ calls int }

func (f *fakeSales) CreateSale(uuid.UUID, []SaleItem) (*SaleResult, error) {
	f.calls++
	return &SaleResult{ID: uuid.New(), TotalAmount: 123}, nil
}

type fakeAnalytics struct{ overview OverviewInfo }

func (f *fakeAnalytics) Overview(uuid.UUID, time.Time, time.Time) (*OverviewInfo, error) {
	return &f.overview, nil
}
func (f *fakeAnalytics) TopProducts(uuid.UUID, time.Time, time.Time, int) ([]TopProductInfo, error) {
	return []TopProductInfo{{ProductName: "Cement", QuantitySold: 5, Profit: 1000}}, nil
}
func (f *fakeAnalytics) SlowMoving(uuid.UUID, int) ([]SlowMovingInfo, error) {
	return nil, nil
}

type fakeInventory struct {
	byProduct map[uuid.UUID]int64
	total     int64
}

func (f *fakeInventory) Quantity(_ uuid.UUID, productID uuid.UUID) (int64, error) {
	return f.byProduct[productID], nil
}
func (f *fakeInventory) TotalQuantity(uuid.UUID) (int64, error) { return f.total, nil }

func newTestService(reply string, products []ProductInfo, sales *fakeSales) Service {
	return NewService(
		&fakeAI{reply: reply},
		&fakeProducts{items: products},
		sales,
		&fakeAnalytics{overview: OverviewInfo{Revenue: 50000, Profit: 12000, SaleCount: 4, UnitsSold: 9, CustomerCredit: 7500, LowStockCount: 2}},
		&fakeInventory{total: 42},
	)
}

func TestInterpretSalePreviewDoesNotWrite(t *testing.T) {
	sales := &fakeSales{}
	svc := newTestService(
		`{"intent":"record_sale","product_name":"cement","quantity":3}`,
		[]ProductInfo{{ID: uuid.New(), Name: "Cement 50kg", Price: 45050}},
		sales,
	)

	preview, err := svc.Interpret(uuid.New(), "sold 3 cement")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Kind != "sale" || !preview.Understood {
		t.Fatalf("got kind=%q understood=%v, want a sale preview", preview.Kind, preview.Understood)
	}
	if preview.TotalAmount != 135150 {
		t.Fatalf("got total %d, want 135150", preview.TotalAmount)
	}
	if sales.calls != 0 {
		t.Fatalf("Interpret wrote to the database (%d sale calls); writes must wait for Confirm", sales.calls)
	}
}

func TestConfirmPerformsWrite(t *testing.T) {
	sales := &fakeSales{}
	svc := newTestService("", nil, sales)

	if _, err := svc.Confirm(uuid.New(), uuid.New(), 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sales.calls != 1 {
		t.Fatalf("got %d sale calls, want 1 after Confirm", sales.calls)
	}
}

func TestAnswerProfitToday(t *testing.T) {
	svc := newTestService(`{"intent":"profit_today"}`, nil, &fakeSales{})

	preview, err := svc.Interpret(uuid.New(), "what is the profit today")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Kind != "answer" {
		t.Fatalf("got kind %q, want answer", preview.Kind)
	}
	if !strings.Contains(preview.Message, "120.00") {
		t.Fatalf("message %q does not contain the computed profit", preview.Message)
	}
}

func TestAnswerCategoryCount(t *testing.T) {
	svc := newTestService(
		`{"intent":"category_count"}`,
		[]ProductInfo{
			{Name: "Cement 50kg", Category: "Building"},
			{Name: "Hammer", Category: "Tools"},
			{Name: "Nails", Category: "Building"},
		},
		&fakeSales{},
	)

	preview, err := svc.Interpret(uuid.New(), "how many product categories do i have?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(preview.Message, "2") || !strings.Contains(preview.Message, "Building") {
		t.Fatalf("message %q does not reflect 2 distinct categories", preview.Message)
	}
}

func TestUnknownIntentGivesHelp(t *testing.T) {
	svc := newTestService(`{"intent":"unknown"}`, nil, &fakeSales{})

	preview, err := svc.Interpret(uuid.New(), "hello there")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Message != unknownAnswer {
		t.Fatalf("got %q, want the help message", preview.Message)
	}
}

func TestAnswerProductStockWithPhrase(t *testing.T) {
	cementID := uuid.New()
	svc := NewService(
		&fakeAI{reply: `{"intent":"product_stock","product_name":"bags of cement"}`},
		&fakeProducts{items: []ProductInfo{{ID: cementID, Name: "Cement 50kg", Unit: "bag", Price: 45050}}},
		&fakeSales{},
		&fakeAnalytics{},
		&fakeInventory{byProduct: map[uuid.UUID]int64{cementID: 17}, total: 30},
	)

	preview, err := svc.Interpret(uuid.New(), "how many bags of cement are left?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(preview.Message, "Cement 50kg") || !strings.Contains(preview.Message, "17 bags") {
		t.Fatalf("got %q, want the cement stock level of 17 bags", preview.Message)
	}
}

func TestAnswerTotalStock(t *testing.T) {
	svc := NewService(
		&fakeAI{reply: `{"intent":"total_stock"}`},
		&fakeProducts{items: []ProductInfo{{Name: "Cement 50kg"}, {Name: "Hammer"}}},
		&fakeSales{},
		&fakeAnalytics{},
		&fakeInventory{total: 42},
	)

	preview, err := svc.Interpret(uuid.New(), "how many items are left?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(preview.Message, "42") || !strings.Contains(preview.Message, "2 product") {
		t.Fatalf("got %q, want 42 units across 2 products", preview.Message)
	}
}

func TestAnswerProductPrice(t *testing.T) {
	svc := NewService(
		&fakeAI{reply: `{"intent":"product_price","product_name":"cement"}`},
		&fakeProducts{items: []ProductInfo{{ID: uuid.New(), Name: "Cement 50kg", Unit: "bag", Price: 45050}}},
		&fakeSales{},
		&fakeAnalytics{},
		&fakeInventory{},
	)

	preview, err := svc.Interpret(uuid.New(), "what is the price a single bag of cement?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(preview.Message, "450.50") || !strings.Contains(preview.Message, "bag") {
		t.Fatalf("got %q, want the cement price per bag", preview.Message)
	}
}
