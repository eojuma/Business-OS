-- +migrate Up
-- A queue timeout is ambiguous: the payout may still complete, so it must not be
-- recorded as a terminal failure. A later result callback may replace it.
DO $$
DECLARE
    existing_constraint text;
BEGIN
    FOR existing_constraint IN
        SELECT con.conname
        FROM pg_constraint con
        JOIN pg_class rel ON rel.oid = con.conrelid
        JOIN pg_attribute att ON att.attrelid = rel.oid AND att.attnum = ANY (con.conkey)
        WHERE rel.relname = 'mpesa_transactions'
          AND con.contype = 'c'
          AND att.attname = 'status'
    LOOP
        EXECUTE format('ALTER TABLE mpesa_transactions DROP CONSTRAINT %I', existing_constraint);
    END LOOP;
END $$;

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

DO $$
DECLARE
    existing_constraint text;
BEGIN
    FOR existing_constraint IN
        SELECT con.conname
        FROM pg_constraint con
        JOIN pg_class rel ON rel.oid = con.conrelid
        JOIN pg_attribute att ON att.attrelid = rel.oid AND att.attnum = ANY (con.conkey)
        WHERE rel.relname = 'mpesa_transactions'
          AND con.contype = 'c'
          AND att.attname = 'status'
    LOOP
        EXECUTE format('ALTER TABLE mpesa_transactions DROP CONSTRAINT %I', existing_constraint);
    END LOOP;
END $$;

ALTER TABLE mpesa_transactions ADD CONSTRAINT mpesa_transactions_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'cancelled'));
