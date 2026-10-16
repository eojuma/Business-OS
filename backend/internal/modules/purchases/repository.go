package purchases

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InventoryReceiver interface {
	RecordMovementTx(tx *gorm.DB, businessID, productID uuid.UUID, movementType string, quantity int64, note string) error
}
type SupplierCharger interface {
	AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error
	SetPaymentDueDateTx(tx *gorm.DB, id, businessID uuid.UUID, receivedAt time.Time) (*time.Time, error)
}
type ProductCostUpdater interface {
	UpdateCostPriceTx(tx *gorm.DB, businessID, productID uuid.UUID, costPrice int64) error
}

type Repository interface {
	Create(purchase *Purchase, items []PurchaseLineItem) error
	FindByID(id, businessID uuid.UUID) (*Purchase, error)
	List(businessID uuid.UUID, supplierID *uuid.UUID) ([]Purchase, error)
	Receive(id, businessID uuid.UUID, inventory InventoryReceiver, suppliers SupplierCharger, products ProductCostUpdater) (*Purchase, error)
	ListPaginated(businessID uuid.UUID, page, limit int, status *string, supplierID *uuid.UUID, dateFrom *string, dateTo *string) ([]Purchase, int64, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(purchase *Purchase, items []PurchaseLineItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(purchase).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].PurchaseID = purchase.ID
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		purchase.Items = items
		return nil
	})
}

func (r *repository) FindByID(id, businessID uuid.UUID) (*Purchase, error) {
	var purchase Purchase
	err := r.db.Preload("Items").Where("id = ? AND business_id = ?", id, businessID).First(&purchase).Error
	return &purchase, err
}

func (r *repository) List(businessID uuid.UUID, supplierID *uuid.UUID) ([]Purchase, error) {
	var purchases []Purchase
	query := r.db.Preload("Items").Where("business_id = ?", businessID)
	if supplierID != nil {
		query = query.Where("supplier_id = ?", *supplierID)
	}
	err := query.Order("created_at desc").Find(&purchases).Error
	return purchases, err
}

func (r *repository) ListPaginated(businessID uuid.UUID, page, limit int, status *string, supplierID *uuid.UUID, dateFrom *string, dateTo *string) ([]Purchase, int64, error) {
	var purchases []Purchase
	query := r.db.Preload("Items").Where("business_id = ?", businessID)
	if supplierID != nil {
		query = query.Where("supplier_id = ?", *supplierID)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if dateFrom != nil {
		query = query.Where("created_at >= ?", dateFrom)
	}
	if dateTo != nil {
		query = query.Where("created_at <= ?", dateTo)
	}
	var total int64
	query.Count(&total)
	query = query.Offset((page - 1) * limit).Limit(limit).Order("created_at desc")
	err := query.Find(&purchases).Error
	if err != nil {
		return nil, 0, err
	}
	return purchases, total, nil
}

func (r *repository) Receive(id, businessID uuid.UUID, inventory InventoryReceiver, suppliers SupplierCharger, products ProductCostUpdater) (*Purchase, error) {
	var purchase Purchase
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Items").Where("id = ? AND business_id = ?", id, businessID).First(&purchase).Error; err != nil {
			return err
		}
		if purchase.Status == "received" {
			return ErrAlreadyReceived
		}
		for _, item := range purchase.Items {
			if err := inventory.RecordMovementTx(tx, businessID, item.ProductID, "restock", item.Quantity, "purchase "+purchase.ID.String()); err != nil {
				return err
			}
			if err := products.UpdateCostPriceTx(tx, businessID, item.ProductID, item.UnitCost); err != nil {
				return err
			}
		}
		now := time.Now()
		outstanding := purchase.TotalAmount - purchase.AmountPaid
		if outstanding > 0 {
			if err := suppliers.AddOutstandingTx(tx, purchase.SupplierID, businessID, outstanding); err != nil {
				return err
			}
		}
		dueDate, err := suppliers.SetPaymentDueDateTx(tx, purchase.SupplierID, businessID, now)
		if err != nil {
			return err
		}
		purchase.Status = "received"
		purchase.ReceivedAt = &now
		purchase.DueDate = dueDate
		return tx.Save(&purchase).Error
	})
	return &purchase, err
}
