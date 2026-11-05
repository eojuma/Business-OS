package sales

import (
	"testing"
	"time"
)

func TestParseSaleDate(t *testing.T) {
	dateOnly, err := parseSaleDate("2026-11-30")
	if err != nil {
		t.Fatalf("parse date-only: %v", err)
	}
	wantEndOfDay := time.Date(2026, 11, 30, 23, 59, 59, 999999999, time.UTC)
	if !dateOnly.Equal(wantEndOfDay) {
		t.Fatalf("date-only = %s, want %s", dateOnly, wantEndOfDay)
	}

	timestamp, err := parseSaleDate("2026-11-30T12:00:00Z")
	if err != nil {
		t.Fatalf("parse timestamp: %v", err)
	}
	if !timestamp.Equal(time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("timestamp = %s", timestamp)
	}

	if _, err := parseSaleDate("not-a-date"); err == nil {
		t.Fatal("expected an invalid date to fail")
	}
}
