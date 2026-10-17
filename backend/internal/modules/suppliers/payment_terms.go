package suppliers

import "time"

const (
	PaymentTermsNet15        = "net15"
	PaymentTermsNet30        = "net30"
	PaymentTermsNet45        = "net45"
	PaymentTermsNet60        = "net60"
	PaymentTermsNet90        = "net90"
	PaymentTermsDueOnReceipt = "due_on_receipt"

	PaymentTermsDefault = PaymentTermsNet30
)

var paymentTermsDays = map[string]int{
	PaymentTermsNet15:        15,
	PaymentTermsNet30:        30,
	PaymentTermsNet45:        45,
	PaymentTermsNet60:        60,
	PaymentTermsNet90:        90,
	PaymentTermsDueOnReceipt: 0,
}

// IsValidPaymentTerms reports whether terms is one of the supported payment terms.
func IsValidPaymentTerms(terms string) bool {
	_, ok := paymentTermsDays[terms]
	return ok
}

// DueDateFrom computes the due date for an invoice received at receivedAt under
// the given payment terms. It returns ErrInvalidPaymentTerms for unknown terms.
func DueDateFrom(receivedAt time.Time, terms string) (time.Time, error) {
	days, ok := paymentTermsDays[terms]
	if !ok {
		return time.Time{}, ErrInvalidPaymentTerms
	}
	return receivedAt.AddDate(0, 0, days), nil
}
