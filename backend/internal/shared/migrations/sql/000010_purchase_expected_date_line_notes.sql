-- +migrate Up
-- +migrate NoTransaction
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS expected_at TIMESTAMPTZ;
ALTER TABLE purchase_line_items ADD COLUMN IF NOT EXISTS note TEXT;

-- A failed CREATE INDEX CONCURRENTLY can leave an invalid index behind;
-- drop it first so a retry does not skip a broken index.
DO $$
BEGIN
    IF to_regclass('idx_purchases_expected_at') IS NOT NULL AND EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_purchases_expected_at') AND NOT indisvalid
    ) THEN
        DROP INDEX idx_purchases_expected_at;
    END IF;
END
$$;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_purchases_expected_at ON purchases(expected_at);

-- +migrate Down
DROP INDEX IF EXISTS idx_purchases_expected_at;
ALTER TABLE purchase_line_items DROP COLUMN IF EXISTS note;
ALTER TABLE purchases DROP COLUMN IF EXISTS expected_at;
