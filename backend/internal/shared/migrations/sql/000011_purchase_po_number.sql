-- +migrate Up
-- +migrate NoTransaction
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS po_number TEXT;

-- Per-business, per-year counters make sequence generation atomic and reset
-- automatically on the first purchase of a new year.
CREATE TABLE IF NOT EXISTS purchase_number_counters (
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    year INTEGER NOT NULL,
    last_sequence BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (business_id, year)
);

-- Backfill existing purchases, numbering per business and calendar year.
WITH numbered AS (
    SELECT id,
           business_id,
           EXTRACT(YEAR FROM created_at)::INT AS yr,
           ROW_NUMBER() OVER (
               PARTITION BY business_id, EXTRACT(YEAR FROM created_at)::INT
               ORDER BY created_at, id
           ) AS seq
    FROM purchases
    WHERE po_number IS NULL
)
UPDATE purchases p
SET po_number = 'PO-' || n.yr || '-' || LPAD(n.seq::TEXT, 4, '0')
FROM numbered n
WHERE p.id = n.id;

-- Seed the annual counters so new purchases continue after the backfill.
INSERT INTO purchase_number_counters (business_id, year, last_sequence)
SELECT business_id, EXTRACT(YEAR FROM created_at)::INT, COUNT(*)
FROM purchases
WHERE po_number IS NOT NULL
GROUP BY business_id, EXTRACT(YEAR FROM created_at)::INT
ON CONFLICT (business_id, year) DO UPDATE
SET last_sequence = GREATEST(purchase_number_counters.last_sequence, EXCLUDED.last_sequence);

ALTER TABLE purchases ALTER COLUMN po_number SET NOT NULL;

-- A failed CREATE INDEX CONCURRENTLY can leave an invalid index behind;
-- drop it first so a retry does not skip a broken index.
DO $$
BEGIN
    IF to_regclass('idx_purchases_business_po_number') IS NOT NULL AND EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_purchases_business_po_number') AND NOT indisvalid
    ) THEN
        DROP INDEX idx_purchases_business_po_number;
    END IF;
END
$$;

-- Enforces uniqueness per business and doubles as the lookup index.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_purchases_business_po_number
    ON purchases(business_id, po_number);

-- +migrate Down
DROP INDEX IF EXISTS idx_purchases_business_po_number;
ALTER TABLE purchases DROP COLUMN IF EXISTS po_number;
DROP TABLE IF EXISTS purchase_number_counters;
