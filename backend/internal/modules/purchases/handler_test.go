package purchases

import (
	"testing"
	"time"
)

func TestParseExpectedAt(t *testing.T) {
	t.Run("date only is anchored at UTC midnight", func(t *testing.T) {
		value := "2026-10-20"
		got, err := parseExpectedAt(&value)
		if err != nil {
			t.Fatalf("parseExpectedAt: %v", err)
		}
		want := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
		if got == nil || !got.Equal(want) {
			t.Fatalf("expected_at = %v, want %v", got, want)
		}
	})

	t.Run("legacy RFC3339 is accepted", func(t *testing.T) {
		value := "2026-10-19T21:00:00Z"
		got, err := parseExpectedAt(&value)
		if err != nil {
			t.Fatalf("parseExpectedAt: %v", err)
		}
		if got == nil || !got.Equal(time.Date(2026, time.October, 19, 21, 0, 0, 0, time.UTC)) {
			t.Fatalf("expected_at = %v, want 2026-10-19T21:00:00Z", got)
		}
	})

	t.Run("empty value is nil", func(t *testing.T) {
		value := "  "
		got, err := parseExpectedAt(&value)
		if err != nil || got != nil {
			t.Fatalf("parseExpectedAt = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("invalid value is rejected", func(t *testing.T) {
		value := "20-10-2026"
		if _, err := parseExpectedAt(&value); err == nil {
			t.Fatal("expected an error for an invalid date")
		}
	})
}
