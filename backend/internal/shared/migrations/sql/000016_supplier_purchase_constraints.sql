-- +migrate Up
-- Replace the implicit foreign keys with explicit ON DELETE behavior. The
-- constraints are added NOT VALID so this migration does not scan or block
-- writes on populated tables; 000017 validates them after the fact.
ALTER TABLE purchases
    DROP CONSTRAINT IF EXISTS purchases_supplier_id_fkey;
ALTER TABLE purchases
    ADD CONSTRAINT purchases_supplier_id_fkey
    FOREIGN KEY (supplier_id) REFERENCES suppliers(id) ON DELETE RESTRICT NOT VALID;

ALTER TABLE purchase_line_items
    DROP CONSTRAINT IF EXISTS purchase_line_items_purchase_id_fkey;
ALTER TABLE purchase_line_items
    ADD CONSTRAINT purchase_line_items_purchase_id_fkey
    FOREIGN KEY (purchase_id) REFERENCES purchases(id) ON DELETE CASCADE NOT VALID;

ALTER TABLE purchase_line_items
    DROP CONSTRAINT IF EXISTS purchase_line_items_product_id_fkey;
ALTER TABLE purchase_line_items
    ADD CONSTRAINT purchase_line_items_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT NOT VALID;

-- +migrate Down
-- Restore the base constraint definitions from 000002_operations_modules.sql.
ALTER TABLE purchase_line_items
    DROP CONSTRAINT IF EXISTS purchase_line_items_product_id_fkey;
ALTER TABLE purchase_line_items
    DROP CONSTRAINT IF EXISTS purchase_line_items_purchase_id_fkey;
ALTER TABLE purchases
    DROP CONSTRAINT IF EXISTS purchases_supplier_id_fkey;

ALTER TABLE purchases
    ADD CONSTRAINT purchases_supplier_id_fkey
    FOREIGN KEY (supplier_id) REFERENCES suppliers(id);
ALTER TABLE purchase_line_items
    ADD CONSTRAINT purchase_line_items_purchase_id_fkey
    FOREIGN KEY (purchase_id) REFERENCES purchases(id) ON DELETE CASCADE;
ALTER TABLE purchase_line_items
    ADD CONSTRAINT purchase_line_items_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products(id);
