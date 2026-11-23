-- +migrate Up
-- A queue timeout is ambiguous: the payout may still complete, so it must not be
-- recorded as a terminal failure. A later result callback may replace it.
-- Drop any existing status check constraint (idempotent) so the migration is safe
-- to reapply across schemas where the constraint name may collide.
ALTER TABLE mpesa_transactions DROP CONSTRAINT IF EXISTS mpesa_transactions_status_check;

ALTER TABLE mpesa_transactions ADD CONSTRAINT mpesa_transactions_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'cancelled', 'timed_out'));

-- B2C payouts remember their intended target so the result callback can settle
-- a supplier payable instead of guessing at an operating expense.
ALTER TABLE mpesa_transactions ADD COLUMN IF NOT EXISTS supplier_id UUID REFERENCES suppliers(id) ON DELETE SET NULL;
ALTER TABLE mpesa_transactions ADD COLUMN IF NOT EXISTS expense_category TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_supplier ON mpesa_transactions(supplier_id) WHERE supplier_id IS NOT NULL;

-- +migrate Down
DROP INDEX IF EXISTS idx_mpesa_transactions_supplier;

ALTER TABLE mpesa_transactions DROP COLUMN IF EXISTS expense_category;
ALTER TABLE mpesa_transactions DROP COLUMN IF EXISTS supplier_id;

-- Timed-out rows have no equivalent in the old schema; record them as failed.
UPDATE mpesa_transactions SET status = 'failed' WHERE status = 'timed_out';

ALTER TABLE mpesa_transactions DROP CONSTRAINT IF EXISTS mpesa_transactions_status_check;

ALTER TABLE mpesa_transactions ADD CONSTRAINT mpesa_transactions_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'cancelled'));
