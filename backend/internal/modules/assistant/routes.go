package assistant

import (
	"errors"
	"log"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/analytics"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/sales"
	"github.com/businessos/backend/internal/modules/suppliers"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
		infos[i] = ProductInfo{
			ID:       p.ID,
			Name:     p.Name,
			Price:    p.Price,
			Unit:     p.Unit,
			Category: p.Category,
		}
	}
	return infos, nil
}

type saleAdapter struct {
	svc sales.Service
}

func (a *saleAdapter) CreateSale(businessID uuid.UUID, items []SaleItem) (*SaleResult, error) {
	saleItems := make([]sales.SaleItemInput, len(items))
	for i, item := range items {
		saleItems[i] = sales.SaleItemInput{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
		}
	}

	result, err := a.svc.CreateSale(sales.CreateSaleInput{
		BusinessID: businessID,
		Items:      saleItems,
	})
	if err != nil {
		return nil, err
	}

	return &SaleResult{
		ID:          result.ID,
		TotalAmount: result.TotalAmount,
	}, nil
}

// analyticsAdapter exposes only the read-only aggregates the assistant uses.
type analyticsAdapter struct {
	svc analytics.Service
}

func (a *analyticsAdapter) Overview(businessID uuid.UUID, from, to time.Time) (*OverviewInfo, error) {
	o, err := a.svc.Overview(businessID, from, to)
	if err != nil {
		return nil, err
	}
	return &OverviewInfo{
		Revenue:        o.Revenue,
		Profit:         o.Profit,
		SaleCount:      o.SaleCount,
		UnitsSold:      o.UnitsSold,
		CustomerCredit: o.CustomerCredit,
		LowStockCount:  o.LowStockCount,
	}, nil
}

func (a *analyticsAdapter) TopProducts(businessID uuid.UUID, from, to time.Time, limit int) ([]TopProductInfo, error) {
	list, err := a.svc.TopProducts(businessID, from, to, limit)
	if err != nil {
		return nil, err
	}
	infos := make([]TopProductInfo, len(list))
	for i, p := range list {
		infos[i] = TopProductInfo{
			ProductName:  p.ProductName,
			QuantitySold: p.QuantitySold,
			Revenue:      p.Revenue,
			Profit:       p.Profit,
		}
	}
	return infos, nil
}

func (a *analyticsAdapter) SlowMoving(businessID uuid.UUID, days int) ([]SlowMovingInfo, error) {
	list, err := a.svc.SlowMoving(businessID, days)
	if err != nil {
		return nil, err
	}
	infos := make([]SlowMovingInfo, len(list))
	for i, p := range list {
		infos[i] = SlowMovingInfo{
			ProductName:    p.ProductName,
			QuantityOnHand: p.QuantityOnHand,
			DaysSinceSale:  p.DaysSinceSale,
		}
	}
	return infos, nil
}

// inventoryAdapter is read-only: it can look up stock but never move it.
type inventoryAdapter struct {
	repo inventory.Repository
}

func (a *inventoryAdapter) Quantity(businessID, productID uuid.UUID) (int64, error) {
	level, err := a.repo.GetStockLevel(productID, businessID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return level.Quantity, nil
}

func (a *inventoryAdapter) TotalQuantity(businessID uuid.UUID) (int64, error) {
	return a.repo.TotalStock(businessID)
}

func (a *inventoryAdapter) LowStock(businessID uuid.UUID) ([]StockLevelInfo, error) {
	levels, err := a.repo.ListLowStock(businessID)
	if err != nil {
		return nil, err
	}
	infos := make([]StockLevelInfo, len(levels))
	for i, l := range levels {
		infos[i] = StockLevelInfo{ProductID: l.ProductID, Quantity: l.Quantity, Threshold: l.LowStockThreshold}
	}
	return infos, nil
}

// receivablesAdapter is read-only: it can list debtors but never change balances.
type receivablesAdapter struct {
	repo customers.Repository
}

func (a *receivablesAdapter) Debtors(businessID uuid.UUID) ([]DebtorInfo, error) {
	list, err := a.repo.ListAboveBalance(businessID, 0)
	if err != nil {
		return nil, err
	}
	infos := make([]DebtorInfo, len(list))
	for i, c := range list {
		infos[i] = DebtorInfo{Name: c.Name, Balance: c.Balance}
	}
	return infos, nil
}

// payablesAdapter is read-only: it can list supplier balances but never pay them.
type payablesAdapter struct {
	repo suppliers.Repository
}

func (a *payablesAdapter) SuppliersOwed(businessID uuid.UUID) ([]SupplierDebtInfo, error) {
	list, err := a.repo.List(businessID)
	if err != nil {
		return nil, err
	}
	infos := make([]SupplierDebtInfo, 0, len(list))
	for _, s := range list {
		if s.OutstandingBalance > 0 {
			infos = append(infos, SupplierDebtInfo{Name: s.Name, Balance: s.OutstandingBalance})
		}
	}
	return infos, nil
}

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	productsRepo := products.NewRepository(db)
	salesRepo := sales.NewRepository(db)
	inventoryRepo := inventory.NewRepository(db)
	customersRepo := customers.NewRepository(db)
	suppliersRepo := suppliers.NewRepository(db)
	customersSvc := customers.NewService(customersRepo)

	inventoryMover := sales.NewInventoryAdapter(inventoryRepo)
	customerCharger := sales.NewCustomerAdapter(customersSvc)
	salesSvc := sales.NewService(salesRepo, inventoryMover, productsRepo, customerCharger)

	ai := NewAIClient(cfg)
	log.Printf("assistant: ai base_url=%s model=%s key_set=%t", cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey != "")
	productLister := &productAdapter{repo: productsRepo}
	saleCreator := &saleAdapter{svc: salesSvc}
	analyticsReader := &analyticsAdapter{svc: analytics.NewService(analytics.NewRepository(db))}
	inventoryReader := &inventoryAdapter{repo: inventoryRepo}
	receivablesReader := &receivablesAdapter{repo: customersRepo}
	payablesReader := &payablesAdapter{repo: suppliersRepo}

	svc := NewService(ai, productLister, saleCreator, analyticsReader, inventoryReader, receivablesReader, payablesReader)
	handler := NewHandler(svc)

	group := rg.Group("/assistant")
	{
		group.POST("/interpret", handler.Interpret)
		group.POST("/confirm", handler.Confirm)
	}
}
