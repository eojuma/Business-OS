package purchases

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type testRepository struct{ created *Purchase }

func (r *testRepository) Create(p *Purchase, items []PurchaseLineItem) error {
	r.created = p
	p.Items = items
	return nil
}
func (r *testRepository) FindByID(uuid.UUID, uuid.UUID) (*Purchase, error) {
	return nil, gorm.ErrRecordNotFound
}
func (r *testRepository) List(uuid.UUID, *uuid.UUID) ([]Purchase, error) { return nil, nil }
func (r *testRepository) Receive(uuid.UUID, uuid.UUID, InventoryReceiver, SupplierCharger, ProductCostUpdater) (*Purchase, error) {
	return nil, nil
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
