-- +migrate Up
-- Concurrent C2B deliveries with the same TransID can both insert a row when
-- mpesa_receipt_number is only covered by a plain index. Enforce uniqueness
-- for nonempty receipts so a duplicate insert is rejected at the database
-- boundary instead of racing past the application-level lookup.
CREATE UNIQUE INDEX IF NOT EXISTS idx_mpesa_transactions_receipt_unique
    ON mpesa_transactions(mpesa_receipt_number)
    WHERE mpesa_receipt_number <> '';

-- +migrate Down
DROP INDEX IF EXISTS idx_mpesa_transactions_receipt_unique;