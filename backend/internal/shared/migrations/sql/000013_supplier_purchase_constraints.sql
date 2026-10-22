-- +migrate Up
-- +migrate NoTransaction

-- 1) Supplier name unique per business, respecting soft-delete.
-- Only active (deleted_at IS NULL) suppliers must have a unique name per business.
-- Any existing duplicates would cause this to fail; for a fresh schema (CI) it passes.
-- If duplicates exist in production, they must be resolved before applying.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_suppliers_business_name_active
    ON suppliers(business_id, name)
    WHERE deleted_at IS NULL;

-- 2) Explicitly enforce FK behavior with ON DELETE RESTRICT for supplier references.
-- purchases.supplier_id -> suppliers(id) ON DELETE RESTRICT (prevents deleting supplier with open purchases)
ALTER TABLE purchases
    DROP CONSTRAINT IF EXISTS purchases_supplier_id_fkey;
ALTER TABLE purchases
    ADD CONSTRAINT purchases_supplier_id_fkey
    FOREIGN KEY (supplier_id) REFERENCES suppliers(id) ON DELETE RESTRICT;

-- purchase_line_items.purchase_id already has ON DELETE CASCADE; make it explicit.
ALTER TABLE purchase_line_items
    DROP CONSTRAINT IF EXISTS purchase_line_items_purchase_id_fkey;
ALTER TABLE purchase_line_items
    ADD CONSTRAINT purchase_line_items_purchase_id_fkey
    FOREIGN KEY (purchase_id) REFERENCES purchases(id) ON DELETE CASCADE;

-- purchase_line_items.product_id -> products(id) ON DELETE RESTRICT
ALTER TABLE purchase_line_items
    DROP CONSTRAINT IF EXISTS purchase_line_items_product_id_fkey;
ALTER TABLE purchase_line_items
    ADD CONSTRAINT purchase_line_items_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT;

-- 3) Composite index for (purchase_id, product_id) on line items.
-- Helps queries that filter by both purchase and product.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_purchase_line_items_purchase_product
    ON purchase_line_items(purchase_id, product_id);

-- 5) Partial unique index for soft-deleted supplier name uniqueness
-- (already covered by step 1 above, but documented for clarity)
-- idx_suppliers_business_name_active already created above.

-- +migrate Down
-- Drop constraints in reverse order of creation
ALTER TABLE purchase_line_items DROP CONSTRAINT IF EXISTS purchase_line_items_product_id_fkey;
ALTER TABLE purchase_line_items DROP CONSTRAINT IF EXISTS purchase_line_items_purchase_id_fkey;
ALTER TABLE purchases DROP CONSTRAINT IF EXISTS purchases_supplier_id_fkey;
DROP INDEX IF EXISTS idx_purchase_line_items_purchase_product;
DROP INDEX IF EXISTS idx_suppliers_business_name_active;
