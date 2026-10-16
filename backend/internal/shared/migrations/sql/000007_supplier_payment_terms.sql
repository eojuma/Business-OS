-- +migrate Up
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS payment_terms TEXT NOT NULL DEFAULT 'net30'
    CHECK (payment_terms IN ('net15', 'net30', 'net45', 'net60', 'net90', 'due_on_receipt'));
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS due_date TIMESTAMPTZ;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS due_date TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_suppliers_due_date ON suppliers(due_date);
CREATE INDEX IF NOT EXISTS idx_purchases_due_date ON purchases(due_date);

-- +migrate Down
DROP INDEX IF EXISTS idx_purchases_due_date;
DROP INDEX IF EXISTS idx_suppliers_due_date;
ALTER TABLE purchases DROP COLUMN IF EXISTS due_date;
ALTER TABLE suppliers DROP COLUMN IF EXISTS due_date;
ALTER TABLE suppliers DROP COLUMN IF EXISTS payment_terms;
