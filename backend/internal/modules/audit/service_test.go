package audit

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

type filterRepo struct {
	filter ListFilter
	called bool
}

// Create satisfies the repository interface for filter tests.
func (f *filterRepo) Create(*Entry) error { return nil }

// List records whether the normalized filter reached the repository.
func (f *filterRepo) List(businessID uuid.UUID, filter ListFilter) ([]Entry, int64, error) {
	f.called = true
	f.filter = filter
	return nil, 0, nil
}

// TestServiceRejectsOverflowingPagination documents its scenario and asserts the expected audit behaviour.
func TestServiceRejectsOverflowingPagination(t *testing.T) {
	repo := &filterRepo{}
	svc := NewService(repo)

	if _, _, err := svc.List(uuid.New(), ListFilter{Page: math.MaxInt, Limit: 20}); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidPagination)
	}
	if repo.called {
		t.Fatal("repository must not be called for invalid pagination")
	}
}

// TestServiceRejectsInvalidDateRange documents its scenario and asserts the expected audit behaviour.
func TestServiceRejectsInvalidDateRange(t *testing.T) {
	repo := &filterRepo{}
	svc := NewService(repo)
	from := time.Now()
	to := from.Add(-time.Hour)

	if _, _, err := svc.List(uuid.New(), ListFilter{DateFrom: &from, DateTo: &to}); !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidDateRange)
	}
	if repo.called {
		t.Fatal("repository must not be called for an invalid date range")
	}
}

// TestServiceNormalizesFilterBeforeListing documents its scenario and asserts the expected audit behaviour.
func TestServiceNormalizesFilterBeforeListing(t *testing.T) {
	repo := &filterRepo{}
	svc := NewService(repo)

	if _, _, err := svc.List(uuid.New(), ListFilter{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !repo.called || repo.filter.Page != 1 || repo.filter.Limit != 20 {
		t.Fatalf("filter = %+v", repo.filter)
	}
}
