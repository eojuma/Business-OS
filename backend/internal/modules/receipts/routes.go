package receipts

import (
	"errors"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/notifications"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/purchases"
	"github.com/businessos/backend/internal/modules/sales"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// productAdapter narrows products.Repository to the read-only view the
// receipts module needs for best-effort matching and ownership checks.
type productAdapter struct {
	repo products.Repository
}

func (a *productAdapter) List(businessID uuid.UUID) ([]ProductInfo, error) {
	list, err := a.repo.List(businessID)
	if err != nil {
		return nil, err
	}
	infos := make([]ProductInfo, len(list))
	for i, p := range list {
		infos[i] = ProductInfo{ID: p.ID, Name: p.Name}
	}
	return infos, nil
}

// supplierAdapter checks supplier ownership without importing the suppliers
// package into the service layer.
type supplierAdapter struct {
	repo suppliers.Repository
}

func (a *supplierAdapter) Exists(businessID, supplierID uuid.UUID) (bool, error) {
	if _, err := a.repo.FindByID(supplierID, businessID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// customerAdapter checks customer ownership without importing the customers
// package into the service layer.
type customerAdapter struct {
	repo customers.Repository
}

func (a *customerAdapter) Exists(businessID, customerID uuid.UUID) (bool, error) {
	if _, err := a.repo.FindByID(customerID, businessID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// purchaseAdapter wraps purchases.Service and translates inputs/results across
// the module boundary.
type purchaseAdapter struct {
	svc purchases.Service
}

func (a *purchaseAdapter) CreatePurchase(input PurchaseInput) (*PurchaseCommitResult, error) {
	items := make([]purchases.ItemInput, len(input.Items))
	for i, item := range input.Items {
		items[i] = purchases.ItemInput{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			UnitCost:  item.UnitPrice,
		}
	}
	purchase, err := a.svc.Create(purchases.CreateInput{
		BusinessID: input.BusinessID,
		SupplierID: input.SupplierID,
		AmountPaid: input.AmountPaid,
		Note:       input.Note,
		Items:      items,
	})
	if err != nil {
		return nil, err
	}
	return &PurchaseCommitResult{ID: purchase.ID, TotalAmount: purchase.TotalAmount}, nil
}

// saleAdapter wraps sales.Service. Sale pricing comes from the catalogue, so
// the reviewed unit price is not passed through.
type saleAdapter struct {
	svc sales.Service
}

func (a *saleAdapter) CreateSale(input SaleInput) (*SaleCommitResult, error) {
	items := make([]sales.SaleItemInput, len(input.Items))
	for i, item := range input.Items {
		items[i] = sales.SaleItemInput{ProductID: item.ProductID, Quantity: item.Quantity}
	}
	sale, err := a.svc.CreateSale(sales.CreateSaleInput{
		BusinessID: input.BusinessID,
		CustomerID: input.CustomerID,
		SaleType:   sales.SaleType(input.SaleType),
		Note:       input.Note,
		Items:      items,
	})
	if err != nil {
		return nil, err
	}
	return &SaleCommitResult{ID: sale.ID, TotalAmount: sale.TotalAmount}, nil
}

// RegisterRoutes wires the receipts module and mounts its endpoints.
func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	productsRepo := products.NewRepository(db)
	inventoryRepo := inventory.NewRepository(db)
	suppliersRepo := suppliers.NewRepository(db)
	customersRepo := customers.NewRepository(db)
	customersSvc := customers.NewService(customersRepo)

	purchasesSvc := purchases.NewService(purchases.NewRepository(db), inventoryRepo, suppliersRepo, productsRepo)
	salesSvc := sales.NewService(
		sales.NewRepository(db),
		sales.NewInventoryAdapter(inventoryRepo),
		productsRepo,
		sales.NewCustomerAdapter(customersSvc),
		notifications.NewGenerator(db),
		sales.NewCustomerResolver(customersSvc),
	)

	svc := NewService(
		NewOCRClient(cfg),
		&productAdapter{repo: productsRepo},
		&supplierAdapter{repo: suppliersRepo},
		&customerAdapter{repo: customersRepo},
		&purchaseAdapter{svc: purchasesSvc},
		&saleAdapter{svc: salesSvc},
	)
	handler := NewHandler(svc)

	group := rg.Group("/receipts")
	{
		group.POST("/scan", handler.Scan)
		group.POST("/commit", handler.Commit)
	}
}
