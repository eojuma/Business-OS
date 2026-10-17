-- +migrate Up
-- Validate the payment-terms constraint separately from 000007 so the brief
-- ACCESS EXCLUSIVE lock from ADD CONSTRAINT is released before the validation
-- scan. VALIDATE CONSTRAINT only takes SHARE UPDATE EXCLUSIVE, which allows
-- concurrent reads and writes.
ALTER TABLE suppliers VALIDATE CONSTRAINT suppliers_payment_terms_check;

-- +migrate Down
-- Validation is not reversible; the constraint itself is dropped by 000007 Down.
