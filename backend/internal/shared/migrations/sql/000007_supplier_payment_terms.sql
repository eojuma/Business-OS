-- +migrate Up
-- +migrate NoTransaction
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS payment_terms TEXT NOT NULL DEFAULT 'net30';
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS due_date TIMESTAMPTZ;
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS last_received_at TIMESTAMPTZ;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS due_date TIMESTAMPTZ;

-- Added NOT VALID so existing rows are not scanned under an ACCESS EXCLUSIVE
-- lock. 000008 validates the constraint in a separate migration.
ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS suppliers_payment_terms_check;
ALTER TABLE suppliers ADD CONSTRAINT suppliers_payment_terms_check
    CHECK (payment_terms IN ('net15', 'net30', 'net45', 'net60', 'net90', 'due_on_receipt'))
    NOT VALID;

-- Backfill due dates for purchases received before this migration.
UPDATE purchases p
SET due_date = p.received_at + INTERVAL '1 day' * (
    CASE s.payment_terms
        WHEN 'due_on_receipt' THEN 0
        WHEN 'net15' THEN 15
        WHEN 'net30' THEN 30
        WHEN 'net45' THEN 45
        WHEN 'net60' THEN 60
        WHEN 'net90' THEN 90
    END)
FROM suppliers s
WHERE p.supplier_id = s.id
  AND p.status = 'received'
  AND p.received_at IS NOT NULL
  AND p.due_date IS NULL;

-- Seed each supplier's latest-receipt tracking from its newest received purchase.
UPDATE suppliers s
SET due_date = x.due_date,
    last_received_at = x.received_at
FROM (
    SELECT DISTINCT ON (supplier_id) supplier_id, received_at, due_date
    FROM purchases
    WHERE status = 'received' AND received_at IS NOT NULL
    ORDER BY supplier_id, received_at DESC
) x
WHERE s.id = x.supplier_id;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_suppliers_due_date ON suppliers(due_date);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_purchases_due_date ON purchases(due_date);

-- +migrate Down
DROP INDEX IF EXISTS idx_purchases_due_date;
DROP INDEX IF EXISTS idx_suppliers_due_date;
ALTER TABLE purchases DROP COLUMN IF EXISTS due_date;
ALTER TABLE suppliers DROP COLUMN IF EXISTS last_received_at;
ALTER TABLE suppliers DROP COLUMN IF EXISTS due_date;
ALTER TABLE suppliers DROP COLUMN IF EXISTS payment_terms;
