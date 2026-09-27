package utils

import "fmt"

// FormatMoney converts cents to a formatted currency string (e.g., "12.34").
func FormatMoney(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	whole := cents / 100
	fraction := cents % 100
	return fmt.Sprintf("%s%d.%02d", sign, whole, fraction)
}

// ToCents converts a decimal amount to cents (e.g., 12.34 -> 1234).
// Rounds to nearest cent.
func ToCents(wholeAndCents float64) int64 {
	return int64(wholeAndCents*100 + 0.5) // +0.5 rounds rather than truncates
}
