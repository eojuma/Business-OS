-- +migrate Up
ALTER TABLE purchase_line_items ADD COLUMN IF NOT EXISTS received_qty BIGINT NOT NULL DEFAULT 0;

-- Existing purchases that were received before partial receiving existed have
-- already had every line delivered, so mark them fully received.
UPDATE purchase_line_items li
SET received_qty = li.quantity
FROM purchases p
WHERE li.purchase_id = p.id
  AND p.status = 'received'
  AND li.received_qty = 0;

ALTER TABLE purchase_line_items DROP CONSTRAINT IF EXISTS purchase_line_items_received_qty_check;
ALTER TABLE purchase_line_items ADD CONSTRAINT purchase_line_items_received_qty_check
    CHECK (received_qty >= 0 AND received_qty <= quantity);

-- +migrate Down
ALTER TABLE purchase_line_items DROP CONSTRAINT IF EXISTS purchase_line_items_received_qty_check;
ALTER TABLE purchase_line_items DROP COLUMN IF EXISTS received_qty;
