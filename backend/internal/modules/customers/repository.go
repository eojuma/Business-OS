package customers

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	Create(c *Customer) error
	FindByID(id, businessID uuid.UUID) (*Customer, error)
	List(businessID uuid.UUID) ([]Customer, error)
	Update(c *Customer) error
	ListAboveBalance(businessID uuid.UUID, threshold int64) ([]Customer, error)
	RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Customer, error)
	ListPayments(id, businessID uuid.UUID) ([]Payment, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Create(c *Customer) error {
	return r.db.Create(c).Error
}

func (r *repository) FindByID(id, businessID uuid.UUID) (*Customer, error) {
	var c Customer
	if err := r.db.Where("id = ? AND business_id = ?", id, businessID).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *repository) List(businessID uuid.UUID) ([]Customer, error) {
	var customers []Customer
	if err := r.db.Where("business_id = ?", businessID).Order("name asc").Find(&customers).Error; err != nil {
		return nil, err
	}
	return customers, nil
}

func (r *repository) Update(c *Customer) error {
	return r.db.Save(c).Error
}

func (r *repository) ListAboveBalance(businessID uuid.UUID, threshold int64) ([]Customer, error) {
	var customers []Customer
	err := r.db.Where("business_id = ? AND balance > ?", businessID, threshold).
		Order("balance desc").Find(&customers).Error
	if err != nil {
		return nil, err
	}
	return customers, nil
}

func (r *repository) RecordPayment(id, businessID uuid.UUID, payment *Payment) (*Customer, error) {
	var customer Customer
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND business_id = ?", id, businessID).First(&customer).Error; err != nil {
			return err
		}
		if payment.Amount > customer.Balance {
			return ErrPaymentExceedsBalance
		}
		customer.Balance -= payment.Amount
		payment.BusinessID, payment.CustomerID = businessID, id
		if err := tx.Save(&customer).Error; err != nil {
			return err
		}
		return tx.Create(payment).Error
	})
	return &customer, err
}

func (r *repository) ListPayments(id, businessID uuid.UUID) ([]Payment, error) {
	var payments []Payment
	err := r.db.Where("customer_id = ? AND business_id = ?", id, businessID).Order("created_at desc").Find(&payments).Error
	return payments, err
}
