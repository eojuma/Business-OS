package products

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type testRepository struct {
	products map[uuid.UUID]*Product
}

func newTestRepository() *testRepository {
	return &testRepository{
		products: make(map[uuid.UUID]*Product),
	}
}

func (r *testRepository) Create(p *Product) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	r.products[p.ID] = p
	return nil
}

func (r *testRepository) FindByID(id, businessID uuid.UUID) (*Product, error) {
	p, ok := r.products[id]
	if !ok || p.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return p, nil
}

func (r *testRepository) List(businessID uuid.UUID) ([]Product, error) {
	var result []Product
	for _, p := range r.products {
		if p.BusinessID == businessID {
			result = append(result, *p)
		}
	}
	return result, nil
}

func (r *testRepository) Update(p *Product) error {
	if _, ok := r.products[p.ID]; !ok {
		return gorm.ErrRecordNotFound
	}
	r.products[p.ID] = p
	return nil
}

func (r *testRepository) Delete(id, businessID uuid.UUID) error {
	p, ok := r.products[id]
	if !ok || p.BusinessID != businessID {
		return gorm.ErrRecordNotFound
	}
	delete(r.products, id)
	return nil
}

func (r *testRepository) GetPricing(businessID, productID uuid.UUID) (int64, int64, error) {
	p, ok := r.products[productID]
	if !ok || p.BusinessID != businessID {
		return 0, 0, gorm.ErrRecordNotFound
	}
	return p.Price, p.CostPrice, nil
}

func (r *testRepository) UpdateCostPriceTx(tx *gorm.DB, businessID, productID uuid.UUID, costPrice int64) error {
	p, ok := r.products[productID]
	if !ok || p.BusinessID != businessID {
		return gorm.ErrRecordNotFound
	}
	p.CostPrice = costPrice
	return nil
}

func TestCreateProduct(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	input := CreateInput{
		BusinessID: businessID,
		Name:       "Test Product",
		Category:   "Test Category",
		Unit:       "pcs",
		Price:      15000,
		SKU:        "TEST-001",
	}

	product, err := svc.Create(input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if product.ID == uuid.Nil {
		t.Fatal("expected product ID to be set")
	}
	if product.BusinessID != businessID {
		t.Fatalf("business_id = %v, want %v", product.BusinessID, businessID)
	}
	if product.Name != "Test Product" {
		t.Fatalf("name = %v, want 'Test Product'", product.Name)
	}
	if product.Price != 15000 {
		t.Fatalf("price = %v, want 15000", product.Price)
	}
	if product.CostPrice != 0 {
		t.Fatalf("cost_price = %v, want 0 (default)", product.CostPrice)
	}
	if product.SKU != "TEST-001" {
		t.Fatalf("sku = %v, want 'TEST-001'", product.SKU)
	}
}

func TestCreateProductRejectsNegativePrice(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.Create(CreateInput{
		BusinessID: uuid.New(),
		Name:       "Test",
		Unit:       "pcs",
		Price:      -100,
	})
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}
}

func TestGetProduct(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	product := &Product{
		ID:         uuid.New(),
		BusinessID: businessID,
		Name:       "Existing Product",
		Price:      25000,
		CostPrice:  15000,
	}
	repo.products[product.ID] = product

	got, err := svc.Get(product.ID, businessID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "Existing Product" {
		t.Fatalf("name = %v, want 'Existing Product'", got.Name)
	}
	if got.Price != 25000 {
		t.Fatalf("price = %v, want 25000", got.Price)
	}
	if got.CostPrice != 15000 {
		t.Fatalf("cost_price = %v, want 15000", got.CostPrice)
	}
}

func TestGetProductNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.Get(uuid.New(), uuid.New())
	if err != ErrProductNotFound {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestGetProductWrongBusiness(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	product := &Product{ID: uuid.New(), BusinessID: businessID, Name: "Test"}
	repo.products[product.ID] = product

	_, err := svc.Get(product.ID, uuid.New())
	if err != ErrProductNotFound {
		t.Fatalf("expected ErrProductNotFound for wrong business, got %v", err)
	}
}

func TestListProducts(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	repo.products[uuid.New()] = &Product{BusinessID: businessID, Name: "A"}
	repo.products[uuid.New()] = &Product{BusinessID: businessID, Name: "B"}
	repo.products[uuid.New()] = &Product{BusinessID: uuid.New(), Name: "C"}

	list, err := svc.List(businessID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestUpdateProduct(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	product := &Product{ID: uuid.New(), BusinessID: businessID, Name: "Old", Price: 10000}
	repo.products[product.ID] = product

	newName := "Updated Name"
	newPrice := int64(20000)
	updated, err := svc.Update(product.ID, businessID, UpdateInput{
		Name:  &newName,
		Price: &newPrice,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Fatalf("name = %v, want 'Updated Name'", updated.Name)
	}
	if updated.Price != 20000 {
		t.Fatalf("price = %v, want 20000", updated.Price)
	}
}

func TestUpdateProductNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	_, err := svc.Update(uuid.New(), uuid.New(), UpdateInput{})
	if err != ErrProductNotFound {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestUpdateProductRejectsNegativePrice(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	product := &Product{ID: uuid.New(), BusinessID: businessID}
	repo.products[product.ID] = product

	negPrice := int64(-500)
	_, err := svc.Update(product.ID, businessID, UpdateInput{Price: &negPrice})
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}
}

func TestDeleteProduct(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	businessID := uuid.New()
	product := &Product{ID: uuid.New(), BusinessID: businessID}
	repo.products[product.ID] = product

	err := svc.Delete(product.ID, businessID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, ok := repo.products[product.ID]; ok {
		t.Fatal("product should be deleted")
	}
}

func TestDeleteProductNotFound(t *testing.T) {
	repo := newTestRepository()
	svc := NewService(repo)

	err := svc.Delete(uuid.New(), uuid.New())
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}
