package purchases

import (
	"errors"
	"fmt"
	"time"

	"github.com/businessos/backend/internal/modules/suppliers"
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
	Receive(id, businessID uuid.UUID, items []ReceiveItemInput, inventory InventoryReceiver, suppliers SupplierCharger, products ProductCostUpdater) (*Purchase, error)
	ListPaginated(businessID uuid.UUID, page, limit int, status *string, supplierID *uuid.UUID, dateFrom *string, dateTo *string, poNumber *string) ([]Purchase, int64, error)
	Update(id, businessID uuid.UUID, supplierID uuid.UUID, amountPaid int64, note string, expectedAt *time.Time, items []PurchaseLineItem) (*Purchase, error)
	Delete(id, businessID uuid.UUID) error
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(purchase *Purchase, items []PurchaseLineItem) error {
	// Reserve the PO number in the same transaction as the insert so a failed
	// create does not consume a sequence.
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Lock the supplier row for the duration of the insert so purchase
		// creation serializes with supplier archiving. Soft deletion keeps the
		// row, so the foreign key alone would admit purchases that reference
		// an archived supplier.
		var supplier suppliers.Supplier
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", purchase.SupplierID, purchase.BusinessID).
			First(&supplier).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return suppliers.ErrSupplierNotFound
		}
		if err != nil {
			return err
		}

		poNumber, err := nextPONumberTx(tx, purchase.BusinessID, time.Now().Year())
		if err != nil {
			return err
		}
		purchase.PONumber = poNumber
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

// formatPONumber renders PO-{year}-{sequence padded to at least four digits}.
func formatPONumber(year int, sequence int64) string {
	return fmt.Sprintf("PO-%d-%04d", year, sequence)
}

// nextPONumberTx atomically reserves the next sequence for a business and year.
// The upsert locks the counter row, so concurrent creates cannot share a number;
// rollbacks also roll back the reservation.
func nextPONumberTx(tx *gorm.DB, businessID uuid.UUID, year int) (string, error) {
	var sequence int64
	row := tx.Raw(`
		INSERT INTO purchase_number_counters (business_id, year, last_sequence)
		VALUES (?, ?, 1)
		ON CONFLICT (business_id, year)
		DO UPDATE SET last_sequence = purchase_number_counters.last_sequence + 1
		RETURNING last_sequence`, businessID, year).Row()
	if err := row.Scan(&sequence); err != nil {
		return "", err
	}
	return formatPONumber(year, sequence), nil
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

func (r *repository) ListPaginated(businessID uuid.UUID, page, limit int, status *string, supplierID *uuid.UUID, dateFrom *string, dateTo *string, poNumber *string) ([]Purchase, int64, error) {
	var purchases []Purchase
	// Model is set explicitly so Count can infer the table; empty string filters
	// are treated as absent because the HTTP layer always passes their pointers.
	query := r.db.Model(&Purchase{}).Preload("Items").Where("business_id = ?", businessID)
	if supplierID != nil {
		query = query.Where("supplier_id = ?", *supplierID)
	}
	if status != nil && *status != "" {
		query = query.Where("status = ?", *status)
	}
	if dateFrom != nil && *dateFrom != "" {
		query = query.Where("created_at >= ?", *dateFrom)
	}
	if dateTo != nil && *dateTo != "" {
		query = query.Where("created_at <= ?", *dateTo)
	}
	if poNumber != nil && *poNumber != "" {
		query = query.Where("po_number = ?", *poNumber)
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

// receivePlan resolves a receive request into the quantity to receive for each
// line item index. An empty request means "receive every remaining quantity".
func receivePlan(items []PurchaseLineItem, requested []ReceiveItemInput) (map[int]int64, error) {
	plan := make(map[int]int64)
	if len(requested) == 0 {
		for idx := range items {
			if remaining := items[idx].RemainingQty(); remaining > 0 {
				plan[idx] = remaining
			}
		}
		if len(plan) == 0 {
			return nil, ErrNothingToReceive
		}
		return plan, nil
	}

	indexesByProduct := make(map[uuid.UUID][]int, len(items))
	for idx := range items {
		productID := items[idx].ProductID
		indexesByProduct[productID] = append(indexesByProduct[productID], idx)
	}

	seen := make(map[uuid.UUID]bool, len(requested))
	for _, request := range requested {
		if request.ReceivedQty <= 0 {
			return nil, ErrInvalidReceiveQuantity
		}
		if seen[request.ProductID] {
			return nil, ErrDuplicateReceiveItem
		}
		seen[request.ProductID] = true

		indexes, ok := indexesByProduct[request.ProductID]
		if !ok {
			return nil, ErrUnknownReceiveItem
		}
		// Distribute across duplicate-product lines in line order so a receipt
		// can consume the remaining quantity on every matching line.
		remaining := request.ReceivedQty
		for _, idx := range indexes {
			if remaining == 0 {
				break
			}
			available := items[idx].RemainingQty()
			if available <= 0 {
				continue
			}
			take := min(remaining, available)
			plan[idx] += take
			remaining -= take
		}
		if remaining > 0 {
			return nil, ErrInvalidReceiveQuantity
		}
	}
	return plan, nil
}

func (r *repository) Receive(id, businessID uuid.UUID, items []ReceiveItemInput, inventory InventoryReceiver, suppliers SupplierCharger, products ProductCostUpdater) (*Purchase, error) {
	var purchase Purchase
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Items").Where("id = ? AND business_id = ?", id, businessID).First(&purchase).Error; err != nil {
			return err
		}
		if purchase.Status == "received" {
			return ErrAlreadyReceived
		}

		plan, err := receivePlan(purchase.Items, items)
		if err != nil {
			return err
		}

		var previouslyReceivedValue int64
		for _, item := range purchase.Items {
			previouslyReceivedValue += item.ReceivedQty * item.UnitCost
		}

		var newlyReceivedValue int64
		for idx, quantity := range plan {
			item := &purchase.Items[idx]
			item.ReceivedQty += quantity
			newlyReceivedValue += quantity * item.UnitCost
			if err := inventory.RecordMovementTx(tx, businessID, item.ProductID, "restock", quantity, "purchase "+purchase.ID.String()); err != nil {
				return err
			}
			if err := products.UpdateCostPriceTx(tx, businessID, item.ProductID, item.UnitCost); err != nil {
				return err
			}
			if err := tx.Model(&PurchaseLineItem{}).Where("id = ?", item.ID).
				Update("received_qty", item.ReceivedQty).Error; err != nil {
				return err
			}
		}

		// Charge the supplier only for the value newly delivered, applying any
		// amount already paid to the earliest receipts so partial receipts never
		// overcharge the outstanding balance.
		totalReceivedValue := previouslyReceivedValue + newlyReceivedValue
		charge := max(int64(0), totalReceivedValue-purchase.AmountPaid) -
			max(int64(0), previouslyReceivedValue-purchase.AmountPaid)
		if charge > 0 {
			if err := suppliers.AddOutstandingTx(tx, purchase.SupplierID, businessID, charge); err != nil {
				return err
			}
		}

		now := time.Now()
		if purchase.FullyReceived() {
			purchase.Status = "received"
			purchase.ReceivedAt = &now
		}
		dueDate, err := suppliers.SetPaymentDueDateTx(tx, purchase.SupplierID, businessID, now)
		if err != nil {
			return err
		}
		purchase.DueDate = dueDate
		return tx.Model(&Purchase{}).Where("id = ? AND business_id = ?", id, businessID).Updates(map[string]interface{}{
			"status":      purchase.Status,
			"received_at": purchase.ReceivedAt,
			"due_date":    purchase.DueDate,
		}).Error
	})
	return &purchase, err
}

// Update replaces the editable fields of a draft purchase. It refuses to touch
// a purchase that has already been received, or one where any line item has
// stock against it, because changing quantities then would corrupt receipts.
func (r *repository) Update(id, businessID uuid.UUID, supplierID uuid.UUID, amountPaid int64, note string, expectedAt *time.Time, items []PurchaseLineItem) (*Purchase, error) {
	var purchase Purchase
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Items").
			Where("id = ? AND business_id = ?", id, businessID).First(&purchase).Error; err != nil {
			return err
		}
		if purchase.Status == "received" {
			return ErrAlreadyReceived
		}
		for _, item := range purchase.Items {
			if item.ReceivedQty > 0 {
				return ErrPurchasePartiallyReceived
			}
		}

		// Like create, only an active supplier belonging to this business may be
		// referenced; the row is locked so it cannot be archived concurrently.
		var supplier suppliers.Supplier
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", supplierID, businessID).
			First(&supplier).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return suppliers.ErrSupplierNotFound
		}
		if err != nil {
			return err
		}

		if err := tx.Where("purchase_id = ?", purchase.ID).Delete(&PurchaseLineItem{}).Error; err != nil {
			return err
		}

		var total int64
		for i := range items {
			items[i].PurchaseID = purchase.ID
			items[i].ReceivedQty = 0
			total += items[i].Subtotal
		}
		if amountPaid < 0 || amountPaid > total {
			return ErrInvalidAmountPaid
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		if err := tx.Model(&Purchase{}).Where("id = ? AND business_id = ?", purchase.ID, businessID).
			Updates(map[string]interface{}{
				"supplier_id":  supplierID,
				"amount_paid":  amountPaid,
				"note":         note,
				"expected_at":  expectedAt,
				"total_amount": total,
			}).Error; err != nil {
			return err
		}
		return tx.Preload("Items").Where("id = ? AND business_id = ?", id, businessID).First(&purchase).Error
	})
	return &purchase, err
}

// Delete removes a draft purchase and its line items. It refuses to delete a
// received purchase or one with any received quantity recorded.
func (r *repository) Delete(id, businessID uuid.UUID) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var purchase Purchase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Items").
			Where("id = ? AND business_id = ?", id, businessID).First(&purchase).Error; err != nil {
			return err
		}
		if purchase.Status == "received" {
			return ErrAlreadyReceived
		}
		for _, item := range purchase.Items {
			if item.ReceivedQty > 0 {
				return ErrPurchasePartiallyReceived
			}
		}
		if err := tx.Where("purchase_id = ?", purchase.ID).Delete(&PurchaseLineItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND business_id = ?", id, businessID).Delete(&Purchase{}).Error
	})
}
