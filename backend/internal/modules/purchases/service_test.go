package purchases

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type testRepository struct {
	created    *Purchase
	receiveErr error
}

func (r *testRepository) Create(p *Purchase, items []PurchaseLineItem) error {
	r.created = p
	p.Items = items
	return nil
}
func (r *testRepository) FindByID(uuid.UUID, uuid.UUID) (*Purchase, error) {
	return nil, gorm.ErrRecordNotFound
}
func (r *testRepository) List(uuid.UUID, *uuid.UUID) ([]Purchase, error) { return nil, nil }
func (r *testRepository) Receive(uuid.UUID, uuid.UUID, []ReceiveItemInput, InventoryReceiver, SupplierCharger, ProductCostUpdater) (*Purchase, error) {
	return nil, r.receiveErr
}
func (r *testRepository) ListPaginated(uuid.UUID, int, int, *string, *uuid.UUID, *string, *string, *string) ([]Purchase, int64, error) {
	return nil, 0, nil
}

func TestCreateCalculatesPurchaseTotal(t *testing.T) {
	repo := &testRepository{}
	service := NewService(repo, nil, nil, nil)
	purchase, err := service.Create(CreateInput{
		BusinessID: uuid.New(), SupplierID: uuid.New(), AmountPaid: 500,
		Items: []ItemInput{{ProductID: uuid.New(), Quantity: 3, UnitCost: 200}, {ProductID: uuid.New(), Quantity: 2, UnitCost: 150}},
	})
	if err != nil {
		t.Fatalf("create purchase: %v", err)
	}
	if purchase.TotalAmount != 900 {
		t.Fatalf("total = %d, want 900", purchase.TotalAmount)
	}
	if len(purchase.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(purchase.Items))
	}
}

func TestCreateRejectsOverpayment(t *testing.T) {
	service := NewService(&testRepository{}, nil, nil, nil)
	_, err := service.Create(CreateInput{BusinessID: uuid.New(), SupplierID: uuid.New(), AmountPaid: 201, Items: []ItemInput{{ProductID: uuid.New(), Quantity: 1, UnitCost: 200}}})
	if err != ErrInvalidAmountPaid {
		t.Fatalf("error = %v, want %v", err, ErrInvalidAmountPaid)
	}
}

func TestFormatPONumber(t *testing.T) {
	tests := []struct {
		year     int
		sequence int64
		want     string
	}{
		{2026, 1, "PO-2026-0001"},
		{2026, 42, "PO-2026-0042"},
		{2026, 9999, "PO-2026-9999"},
		{2026, 10000, "PO-2026-10000"},
	}
	for _, tt := range tests {
		if got := formatPONumber(tt.year, tt.sequence); got != tt.want {
			t.Fatalf("formatPONumber(%d, %d) = %q, want %q", tt.year, tt.sequence, got, tt.want)
		}
	}
}

func TestReceivePlanAllRemaining(t *testing.T) {
	productA, productB := uuid.New(), uuid.New()
	items := []PurchaseLineItem{
		{ProductID: productA, Quantity: 5, ReceivedQty: 2},
		{ProductID: productB, Quantity: 4, ReceivedQty: 4},
	}

	plan, err := receivePlan(items, nil)
	if err != nil {
		t.Fatalf("receivePlan failed: %v", err)
	}
	// productB is fully received, so only productA remains.
	if len(plan) != 1 || plan[0] != 3 {
		t.Fatalf("plan = %v, want {0:3}", plan)
	}
}

func TestReceivePlanRequestedQuantities(t *testing.T) {
	productA, productB := uuid.New(), uuid.New()
	items := []PurchaseLineItem{
		{ProductID: productA, Quantity: 5, ReceivedQty: 2},
		{ProductID: productB, Quantity: 4, ReceivedQty: 0},
	}

	plan, err := receivePlan(items, []ReceiveItemInput{
		{ProductID: productA, ReceivedQty: 1},
		{ProductID: productB, ReceivedQty: 4},
	})
	if err != nil {
		t.Fatalf("receivePlan failed: %v", err)
	}
	if plan[0] != 1 || plan[1] != 4 {
		t.Fatalf("plan = %v, want {0:1, 1:4}", plan)
	}
}

func TestReceivePlanDistributesAcrossDuplicateProductLines(t *testing.T) {
	product := uuid.New()
	items := []PurchaseLineItem{
		{ProductID: product, Quantity: 2},
		{ProductID: product, Quantity: 3},
	}

	// Four units fill the first line and take two from the second.
	plan, err := receivePlan(items, []ReceiveItemInput{{ProductID: product, ReceivedQty: 4}})
	if err != nil {
		t.Fatalf("receivePlan failed: %v", err)
	}
	if plan[0] != 2 || plan[1] != 2 {
		t.Fatalf("plan = %v, want {0:2, 1:2}", plan)
	}

	// Once the first line is complete, quantity comes from the next line.
	items[0].ReceivedQty = 2
	plan, err = receivePlan(items, []ReceiveItemInput{{ProductID: product, ReceivedQty: 2}})
	if err != nil {
		t.Fatalf("receivePlan failed: %v", err)
	}
	if plan[1] != 2 {
		t.Fatalf("plan = %v, want {1:2}", plan)
	}

	// More than both lines can supply is rejected.
	if _, err := receivePlan(items, []ReceiveItemInput{{ProductID: product, ReceivedQty: 4}}); err != ErrInvalidReceiveQuantity {
		t.Fatalf("error = %v, want %v", err, ErrInvalidReceiveQuantity)
	}
}

func TestReceivePlanRejectsOverReceipt(t *testing.T) {
	productA := uuid.New()
	items := []PurchaseLineItem{{ProductID: productA, Quantity: 5, ReceivedQty: 2}}

	_, err := receivePlan(items, []ReceiveItemInput{{ProductID: productA, ReceivedQty: 4}})
	if err != ErrInvalidReceiveQuantity {
		t.Fatalf("error = %v, want %v", err, ErrInvalidReceiveQuantity)
	}
}

func TestReceivePlanRejectsNonPositiveQuantity(t *testing.T) {
	productA := uuid.New()
	items := []PurchaseLineItem{{ProductID: productA, Quantity: 5}}

	for _, quantity := range []int64{0, -1} {
		_, err := receivePlan(items, []ReceiveItemInput{{ProductID: productA, ReceivedQty: quantity}})
		if err != ErrInvalidReceiveQuantity {
			t.Fatalf("quantity %d: error = %v, want %v", quantity, err, ErrInvalidReceiveQuantity)
		}
	}
}

func TestReceivePlanRejectsUnknownProduct(t *testing.T) {
	items := []PurchaseLineItem{{ProductID: uuid.New(), Quantity: 5}}

	_, err := receivePlan(items, []ReceiveItemInput{{ProductID: uuid.New(), ReceivedQty: 1}})
	if err != ErrUnknownReceiveItem {
		t.Fatalf("error = %v, want %v", err, ErrUnknownReceiveItem)
	}
}

func TestReceivePlanRejectsDuplicateProduct(t *testing.T) {
	productA := uuid.New()
	items := []PurchaseLineItem{{ProductID: productA, Quantity: 5}}

	_, err := receivePlan(items, []ReceiveItemInput{
		{ProductID: productA, ReceivedQty: 1},
		{ProductID: productA, ReceivedQty: 1},
	})
	if err != ErrDuplicateReceiveItem {
		t.Fatalf("error = %v, want %v", err, ErrDuplicateReceiveItem)
	}
}

func TestReceivePlanNothingToReceive(t *testing.T) {
	items := []PurchaseLineItem{{ProductID: uuid.New(), Quantity: 5, ReceivedQty: 5}}

	_, err := receivePlan(items, nil)
	if err != ErrNothingToReceive {
		t.Fatalf("error = %v, want %v", err, ErrNothingToReceive)
	}
}

func TestServiceReceiveMapsNotFound(t *testing.T) {
	service := NewService(&testRepository{receiveErr: gorm.ErrRecordNotFound}, nil, nil, nil)

	_, err := service.Receive(uuid.New(), uuid.New(), ReceiveInput{})
	if err != ErrPurchaseNotFound {
		t.Fatalf("error = %v, want %v", err, ErrPurchaseNotFound)
	}
}
