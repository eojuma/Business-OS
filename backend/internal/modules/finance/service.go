package finance

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidExpense   = errors.New("expense amount must be positive")
	ErrExpenseNotFound  = errors.New("expense not found")
	ErrInvalidDateRange = errors.New("from date must not be after to date")
)

type CreateExpenseInput struct {
	BusinessID            uuid.UUID
	Category, Description string
	Amount                int64
	IncurredAt            time.Time
}
type Service interface {
	CreateExpense(CreateExpenseInput) (*Expense, error)
	ListExpenses(uuid.UUID, time.Time, time.Time) ([]Expense, error)
	DeleteExpense(uuid.UUID, uuid.UUID) error
	Summary(uuid.UUID, time.Time, time.Time) (*Summary, error)
}
type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }
func validateRange(from, to time.Time) error {
	if from.After(to) {
		return ErrInvalidDateRange
	}
	return nil
}
func (s *service) CreateExpense(input CreateExpenseInput) (*Expense, error) {
	if input.Amount <= 0 {
		return nil, ErrInvalidExpense
	}
	when := input.IncurredAt
	if when.IsZero() {
		when = time.Now()
	}
	expense := &Expense{BusinessID: input.BusinessID, Category: input.Category, Description: input.Description, Amount: input.Amount, IncurredAt: when}
	return expense, s.repo.CreateExpense(expense)
}
func (s *service) ListExpenses(id uuid.UUID, from, to time.Time) ([]Expense, error) {
	if err := validateRange(from, to); err != nil {
		return nil, err
	}
	return s.repo.ListExpenses(id, from, to)
}
func (s *service) DeleteExpense(id, businessID uuid.UUID) error {
	return s.repo.DeleteExpense(id, businessID)
}
func (s *service) Summary(id uuid.UUID, from, to time.Time) (*Summary, error) {
	if err := validateRange(from, to); err != nil {
		return nil, err
	}
	return s.repo.Summary(id, from, to)
}
