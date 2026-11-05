package sales

import (
	"time"

	"github.com/businessos/backend/internal/modules/invoices"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InventoryMover interface {
	RecordMovementTx(tx *gorm.DB, businessID, productID uuid.UUID, movementType string, quantity int64, note string) error
}

type Repository interface {
	CreateSale(sale *Sale, lineItems []SaleLineItem, inventory InventoryMover, customers CustomerCharger) error
	FindByID(id, businessID uuid.UUID) (*Sale, error)
	List(businessID uuid.UUID) ([]Sale, error)
}

type CustomerCharger interface {
	ChargeCreditTx(tx *gorm.DB, businessID, customerID uuid.UUID, amount int64) error
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) CreateSale(sale *Sale, lineItems []SaleLineItem, inventory InventoryMover, customers CustomerCharger) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(sale).Error; err != nil {
			return err
		}

		for i := range lineItems {
			lineItems[i].SaleID = sale.ID
			if err := tx.Create(&lineItems[i]).Error; err != nil {
				return err
			}

			// A quotation is not a completed sale: it reserves nothing and
			// moves no stock. Only cash and credit sales decrement inventory.
			if sale.SaleType == SaleTypeQuotation {
				continue
			}

			err := inventory.RecordMovementTx(
				tx,
				sale.BusinessID,
				lineItems[i].ProductID,
				"sale",
				lineItems[i].Quantity,
				"sale "+sale.ID.String(),
			)
			if err != nil {
				return err
			}
		}

		// Only a credit sale puts the balance on the customer's account.
		if sale.SaleType == SaleTypeCredit && sale.CustomerID != nil {
			if err := customers.ChargeCreditTx(tx, sale.BusinessID, *sale.CustomerID, sale.TotalAmount); err != nil {
				return err
			}
		}

		if sale.SaleType != SaleTypeQuotation {
			issueDate := sale.CreatedAt
			if issueDate.IsZero() {
				issueDate = time.Now()
			}
			dueDate := issueDate
			if sale.InvoiceDueDate != nil {
				dueDate = *sale.InvoiceDueDate
			} else if sale.SaleType == SaleTypeCredit {
				dueDate = issueDate.AddDate(0, 0, 30)
			}
			invoice, err := invoices.CreateForSaleTx(tx, invoices.CreateForSaleInput{
				BusinessID: sale.BusinessID,
				SaleID:     sale.ID,
				CustomerID: sale.CustomerID,
				SaleType:   string(sale.SaleType),
				IssueDate:  issueDate,
				DueDate:    dueDate,
				Subtotal:   sale.TotalAmount + sale.Discount,
				Discount:   sale.Discount,
				Total:      sale.TotalAmount,
			})
			if err != nil {
				return err
			}
			sale.Invoice = invoice
		}

		return nil
	})
}

func (r *repository) FindByID(id, businessID uuid.UUID) (*Sale, error) {
	var sale Sale
	err := r.db.Preload("LineItems").
		Where("id = ? AND business_id = ?", id, businessID).
		First(&sale).Error
	if err != nil {
		return nil, err
	}
	return &sale, nil
}

func (r *repository) List(businessID uuid.UUID) ([]Sale, error) {
	var sales []Sale
	err := r.db.Preload("LineItems").
		Where("business_id = ?", businessID).
		Order("created_at desc").
		Find(&sales).Error
	if err != nil {
		return nil, err
	}
	return sales, nil
}
