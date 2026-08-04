package utils

import "fmt"

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

func ToCents(wholeAndCents float64) int64 {
	return int64(wholeAndCents*100 + 0.5) // +0.5 rounds rather than truncates
}