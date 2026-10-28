package suppliers

import (
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	Create(*Supplier) error
	FindByID(id, businessID uuid.UUID) (*Supplier, error)
	List(businessID uuid.UUID, includeArchived bool) ([]Supplier, error)
	ListPaginated(businessID uuid.UUID, page, limit int, search string, includeArchived bool) ([]Supplier, int64, error)
	Update(*Supplier) error
	Delete(id, businessID uuid.UUID) error
	Restore(id, businessID uuid.UUID) error
	RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Supplier, error)
	ListPayments(id, businessID uuid.UUID) ([]Payment, error)
	AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error
	SetPaymentDueDateTx(tx *gorm.DB, id, businessID uuid.UUID, receivedAt time.Time) (*time.Time, error)
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

// paginatedOffset converts a page and page size into a SQL offset. It reports
// false for invalid values or when the multiplication would overflow (checked
// before multiplying, since a wrapped result can also be zero or positive).
func paginatedOffset(page, limit int) (int, bool) {
	if page < 1 || limit < 1 || page-1 > math.MaxInt/limit {
		return 0, false
	}
	return (page - 1) * limit, true
}

// escapeLike makes user input literal inside a LIKE pattern so "%" and "_"
// match themselves instead of acting as wildcards.
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func (r *repository) ListPaginated(businessID uuid.UUID, page, limit int, search string, includeArchived bool) ([]Supplier, int64, error) {
	var suppliers []Supplier
	var total int64

	// Count and page must come from the same snapshot, otherwise concurrent
	// writes can make meta.total disagree with the returned rows.
	err := r.db.Transaction(func(tx *gorm.DB) error {
		query := tx
		if includeArchived {
			query = query.Unscoped()
		}
		query = query.Model(&Supplier{}).Where("business_id = ?", businessID)
		if search != "" {
			term := "%" + escapeLike(strings.ToLower(search)) + "%"
			query = query.Where(
				`LOWER(name) LIKE ? ESCAPE '\' OR LOWER(phone) LIKE ? ESCAPE '\' OR LOWER(email) LIKE ? ESCAPE '\'`,
				term, term, term,
			)
		}

		if err := query.Count(&total).Error; err != nil {
			return err
		}

		offset, ok := paginatedOffset(page, limit)
		if !ok {
			suppliers = []Supplier{}
			return nil
		}

		query = query.Offset(offset).Limit(limit).Order("name asc, id asc")
		return query.Find(&suppliers).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	return suppliers, total, nil
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
