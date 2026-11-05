-- +migrate Up
-- Validate the foreign keys added NOT VALID in 000016. Validation scans the
-- existing rows but does not block reads or writes while it runs.
ALTER TABLE purchases VALIDATE CONSTRAINT purchases_supplier_id_fkey;
ALTER TABLE purchase_line_items VALIDATE CONSTRAINT purchase_line_items_purchase_id_fkey;
ALTER TABLE purchase_line_items VALIDATE CONSTRAINT purchase_line_items_product_id_fkey;

-- +migrate Down
-- Validation has no inverse; the constraints stay enforced and are dropped
-- by the Down block of 000016.
SELECT 1;
