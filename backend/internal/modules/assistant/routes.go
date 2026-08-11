package assistant

import (
	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/sales"
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
			ID:    p.ID,
			Name:  p.Name,
			Price: p.Price,
			Unit:  p.Unit,
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

func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB, cfg *config.Config) {
	productsRepo := products.NewRepository(db)
	salesRepo := sales.NewRepository(db)
	inventoryRepo := inventory.NewRepository(db)
	customersRepo := customers.NewRepository(db)
	customersSvc := customers.NewService(customersRepo)

	inventoryMover := sales.NewInventoryAdapter(inventoryRepo)
	customerCharger := sales.NewCustomerAdapter(customersSvc)
	salesSvc := sales.NewService(salesRepo, inventoryMover, productsRepo, customerCharger)

	ai := NewAIClient(cfg)
	productLister := &productAdapter{repo: productsRepo}
	saleCreator := &saleAdapter{svc: salesSvc}

	svc := NewService(ai, productLister, saleCreator)
	handler := NewHandler(svc)

	group := rg.Group("/assistant")
	{
		group.POST("/interpret", handler.Interpret)
		group.POST("/confirm", handler.Confirm)
	}
}