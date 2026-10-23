package suppliers

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	Create(*Supplier) error
	FindByID(id, businessID uuid.UUID) (*Supplier, error)
	List(businessID uuid.UUID, includeArchived bool) ([]Supplier, error)
	Update(*Supplier) error
	Delete(id, businessID uuid.UUID) error
	Restore(id, businessID uuid.UUID) error
	RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Supplier, error)
	ListPayments(id, businessID uuid.UUID) ([]Payment, error)
	AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error
	SetPaymentDueDateTx(tx *gorm.DB, id, businessID uuid.UUID, receivedAt time.Time) (*time.Time, error)
	GetAging(businessID uuid.UUID, asOf time.Time) (*AgingReport, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(s *Supplier) error { return r.db.Create(s).Error }

func (r *repository) FindByID(id, businessID uuid.UUID) (*Supplier, error) {
	var supplier Supplier
	err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&supplier).Error
	return &supplier, err
}

func (r *repository) List(businessID uuid.UUID, includeArchived bool) ([]Supplier, error) {
	var suppliers []Supplier
	query := r.db
	if includeArchived {
		query = query.Unscoped()
	}
	err := query.Where("business_id = ?", businessID).Order("name asc").Find(&suppliers).Error
	return suppliers, err
}

func (r *repository) Update(s *Supplier) error {
	// Update only the editable columns so an in-flight update cannot upsert over
	// an archive. GORM adds `deleted_at IS NULL` for models with gorm.DeletedAt.
	result := r.db.Model(&Supplier{}).
		Where("id = ? AND business_id = ?", s.ID, s.BusinessID).
		Updates(map[string]interface{}{
			"name":          s.Name,
			"phone":         s.Phone,
			"email":         s.Email,
			"address":       s.Address,
			"payment_terms": s.PaymentTerms,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSupplierNotFound
	}
	return nil
}

func (r *repository) Delete(id, businessID uuid.UUID) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var supplier Supplier
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ?", id, businessID).First(&supplier).Error; err != nil {
			return err
		}

		var purchaseCount int64
		if err := tx.Table("purchases").
			Where("supplier_id = ? AND business_id = ?", id, businessID).
			Count(&purchaseCount).Error; err != nil {
			return err
		}
		if purchaseCount > 0 {
			return ErrSupplierHasPurchases
		}

		return tx.Where("id = ? AND business_id = ?", id, businessID).Delete(&Supplier{}).Error
	})
}

func (r *repository) Restore(id, businessID uuid.UUID) error {
	result := r.db.Unscoped().Model(&Supplier{}).
		Where("id = ? AND business_id = ?", id, businessID).
		Update("deleted_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSupplierNotFound
	}
	return nil
}

func (r *repository) RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Supplier, error) {
	var supplier Supplier
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ?", id, businessID).First(&supplier).Error; err != nil {
			return err
		}
		if payment.Amount > supplier.OutstandingBalance {
			return ErrPaymentExceedsBalance
		}
		supplier.OutstandingBalance -= payment.Amount
		payment.BusinessID, payment.SupplierID = businessID, id
		if err := tx.Save(&supplier).Error; err != nil {
			return err
		}
		return tx.Create(payment).Error
	})
	return &supplier, err
}

func (r *repository) ListPayments(id, businessID uuid.UUID) ([]Payment, error) {
	var payments []Payment
	err := r.db.Where("supplier_id = ? AND business_id = ?", id, businessID).Order("created_at desc").Find(&payments).Error
	return payments, err
}

func (r *repository) AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error {
	result := tx.Model(&Supplier{}).Where("id = ? AND business_id = ?", id, businessID).
		UpdateColumn("outstanding_balance", gorm.Expr("outstanding_balance + ?", amount))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSupplierNotFound
	}
	return nil
}

func (r *repository) SetPaymentDueDateTx(tx *gorm.DB, id, businessID uuid.UUID, receivedAt time.Time) (*time.Time, error) {
	var supplier Supplier
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND business_id = ?", id, businessID).First(&supplier).Error; err != nil {
		return nil, err
	}
	dueDate, err := DueDateFrom(receivedAt, supplier.PaymentTerms)
	if err != nil {
		return nil, err
	}
	// Concurrent receipts can commit out of order; under the row lock, only
	// replace the supplier's due date when this receipt is the latest one.
	if supplier.LastReceivedAt != nil && !receivedAt.After(*supplier.LastReceivedAt) {
		return &dueDate, nil
	}
	result := tx.Model(&Supplier{}).Where("id = ? AND business_id = ?", id, businessID).
		Updates(map[string]interface{}{"due_date": dueDate, "last_received_at": receivedAt})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrSupplierNotFound
	}
	return &dueDate, nil
}


// AgingReport represents the supplier aging report.
type AgingReport struct {
	Current       []AgingSupplier `json:"current"`
	Days1_30      []AgingSupplier `json:"days_1_30"`
	Days31_60     []AgingSupplier `json:"days_31_60"`
	Days61_90     []AgingSupplier `json:"days_61_90"`
	Days90Plus    []AgingSupplier `json:"days_90_plus"`
	Totals        AgingTotals     `json:"totals"`
}

type AgingSupplier struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Outstanding    int64      `json:"outstanding"`
	DueDate        *time.Time `json:"due_date"`
	DaysOverdue    int        `json:"days_overdue"`
}

type AgingTotals struct {
	Current    int64 `json:"current"`
	Days1_30   int64 `json:"days_1_30"`
	Days31_60  int64 `json:"days_31_60"`
	Days61_90  int64 `json:"days_61_90"`
	Days90Plus int64 `json:"days_90_plus"`
	Total      int64 `json:"total"`
}

func daysPastDue(dueDate, asOf time.Time) int {
	due := time.Date(dueDate.Year(), dueDate.Month(), dueDate.Day(), 0, 0, 0, 0, time.UTC)
	ref := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	days := int(ref.Sub(due).Hours() / 24)
	if days < 1 {
		return 1
	}
	return days
}

func (r *repository) GetAging(businessID uuid.UUID, asOf time.Time) (*AgingReport, error) {
	var suppliers []Supplier
	err := r.db.Where("business_id = ? AND deleted_at IS NULL AND outstanding_balance > 0", businessID).
		Order("name asc").Find(&suppliers).Error
	if err != nil {
		return nil, err
	}

	report := &AgingReport{}
	for _, s := range suppliers {
		daysOverdue := 0
		if s.DueDate != nil && s.DueDate.Before(asOf) {
			daysOverdue = daysPastDue(*s.DueDate, asOf)
		}

		entry := AgingSupplier{
			ID:          s.ID,
			Name:        s.Name,
			Outstanding: s.OutstandingBalance,
			DueDate:     s.DueDate,
			DaysOverdue: daysOverdue,
		}

		switch {
		case daysOverdue <= 0:
			report.Current = append(report.Current, entry)
			report.Totals.Current += s.OutstandingBalance
		case daysOverdue <= 30:
			report.Days1_30 = append(report.Days1_30, entry)
			report.Totals.Days1_30 += s.OutstandingBalance
		case daysOverdue <= 60:
			report.Days31_60 = append(report.Days31_60, entry)
			report.Totals.Days31_60 += s.OutstandingBalance
		case daysOverdue <= 90:
			report.Days61_90 = append(report.Days61_90, entry)
			report.Totals.Days61_90 += s.OutstandingBalance
		default:
			report.Days90Plus = append(report.Days90Plus, entry)
			report.Totals.Days90Plus += s.OutstandingBalance
		}
		report.Totals.Total += s.OutstandingBalance
	}
	return report, nil
}
