package finance

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

type testRepository struct{}

func (testRepository) CreateExpense(*Expense) error { return nil }
func (testRepository) ListExpenses(uuid.UUID, time.Time, time.Time) ([]Expense, error) {
	return nil, nil
}
func (testRepository) DeleteExpense(uuid.UUID, uuid.UUID) error { return nil }
func (testRepository) Summary(uuid.UUID, time.Time, time.Time) (*Summary, error) {
	return &Summary{}, nil
}

func TestCreateExpenseRejectsNonPositiveAmount(t *testing.T) {
	service := NewService(testRepository{})
	if _, err := service.CreateExpense(CreateExpenseInput{Amount: 0}); err != ErrInvalidExpense {
		t.Fatalf("error = %v, want %v", err, ErrInvalidExpense)
	}
}

func TestSummaryRejectsInvalidDateRange(t *testing.T) {
	service := NewService(testRepository{})
	now := time.Now()
	if _, err := service.Summary(uuid.New(), now, now.Add(-time.Hour)); err != ErrInvalidDateRange {
		t.Fatalf("error = %v, want %v", err, ErrInvalidDateRange)
	}
}
