package suppliers

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	Create(*Supplier) error
	FindByID(id, businessID uuid.UUID) (*Supplier, error)
	List(businessID uuid.UUID) ([]Supplier, error)
	Update(*Supplier) error
	Delete(id, businessID uuid.UUID) error
	RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Supplier, error)
	ListPayments(id, businessID uuid.UUID) ([]Payment, error)
	AddOutstandingTx(tx *gorm.DB, id, businessID uuid.UUID, amount int64) error
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(s *Supplier) error { return r.db.Create(s).Error }

func (r *repository) FindByID(id, businessID uuid.UUID) (*Supplier, error) {
	var supplier Supplier
	err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&supplier).Error
	return &supplier, err
}

func (r *repository) List(businessID uuid.UUID) ([]Supplier, error) {
	var suppliers []Supplier
	err := r.db.Where("business_id = ?", businessID).Order("name asc").Find(&suppliers).Error
	return suppliers, err
}

func (r *repository) Update(s *Supplier) error { return r.db.Save(s).Error }

func (r *repository) Delete(id, businessID uuid.UUID) error {
	return r.db.Where("id = ? AND business_id = ?", id, businessID).Delete(&Supplier{}).Error
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
