-- +migrate Up
-- +migrate NoTransaction

-- 1) Reconcile duplicate active supplier names before the unique index is
-- created. The oldest supplier keeps its name; later duplicates get a numeric
-- suffix. IDs and purchase references are preserved, so no data is merged or
-- deleted. Suffixes skip names that already exist in the same business.
DO $$
DECLARE
    duplicate RECORD;
    candidate TEXT;
    suffix INTEGER;
BEGIN
    FOR duplicate IN
        SELECT id, business_id, name
        FROM (
            SELECT id,
                   business_id,
                   name,
                   ROW_NUMBER() OVER (
                       PARTITION BY business_id, name
                       ORDER BY created_at, id
                   ) AS rn
            FROM suppliers
            WHERE deleted_at IS NULL
        ) ranked
        WHERE rn > 1
        ORDER BY business_id, name, rn
    LOOP
        suffix := 2;
        LOOP
            candidate := duplicate.name || ' (' || suffix || ')';
            EXIT WHEN NOT EXISTS (
                SELECT 1
                FROM suppliers
                WHERE business_id = duplicate.business_id
                  AND deleted_at IS NULL
                  AND name = candidate
            );
            suffix := suffix + 1;
        END LOOP;

        UPDATE suppliers SET name = candidate WHERE id = duplicate.id;
    END LOOP;
END
$$;

-- 2) Supplier names are unique per business among active (non-deleted) rows.
-- A failed concurrent build can leave an invalid index behind; IF NOT EXISTS
-- would then skip the rebuild on retry, so drop the invalid leftover first.
DO $$
BEGIN
    IF to_regclass('idx_suppliers_business_name_active') IS NOT NULL AND EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_suppliers_business_name_active') AND NOT indisvalid
    ) THEN
        DROP INDEX idx_suppliers_business_name_active;
    END IF;
END
$$;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_suppliers_business_name_active
    ON suppliers(business_id, name)
    WHERE deleted_at IS NULL;

-- 3) Composite index for line items filtered by both purchase and product.
DO $$
BEGIN
    IF to_regclass('idx_purchase_line_items_purchase_product') IS NOT NULL AND EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_purchase_line_items_purchase_product') AND NOT indisvalid
    ) THEN
        DROP INDEX idx_purchase_line_items_purchase_product;
    END IF;
END
$$;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_purchase_line_items_purchase_product
    ON purchase_line_items(purchase_id, product_id);

-- +migrate Down
-- Down always runs inside a transaction, where CONCURRENTLY is not allowed.
DROP INDEX IF EXISTS idx_purchase_line_items_purchase_product;
DROP INDEX IF EXISTS idx_suppliers_business_name_active;
