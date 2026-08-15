-- +migrate Up
ALTER TABLE products ADD COLUMN IF NOT EXISTS cost_price BIGINT NOT NULL DEFAULT 0 CHECK (cost_price >= 0);
ALTER TABLE sale_line_items ADD COLUMN IF NOT EXISTS unit_cost BIGINT NOT NULL DEFAULT 0 CHECK (unit_cost >= 0);

CREATE TABLE IF NOT EXISTS suppliers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    phone TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    outstanding_balance BIGINT NOT NULL DEFAULT 0 CHECK (outstanding_balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS purchases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    supplier_id UUID NOT NULL REFERENCES suppliers(id),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'received')),
    total_amount BIGINT NOT NULL CHECK (total_amount >= 0),
    amount_paid BIGINT NOT NULL DEFAULT 0 CHECK (amount_paid >= 0 AND amount_paid <= total_amount),
    note TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS purchase_line_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_id UUID NOT NULL REFERENCES purchases(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    unit_cost BIGINT NOT NULL CHECK (unit_cost >= 0),
    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS expenses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    amount BIGINT NOT NULL CHECK (amount > 0),
    incurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


CREATE INDEX IF NOT EXISTS idx_suppliers_business_id ON suppliers(business_id);
CREATE INDEX IF NOT EXISTS idx_purchases_business_id ON purchases(business_id);
CREATE INDEX IF NOT EXISTS idx_purchases_supplier_id ON purchases(supplier_id);
CREATE INDEX IF NOT EXISTS idx_purchases_received_at ON purchases(received_at);
CREATE INDEX IF NOT EXISTS idx_purchase_line_items_purchase_id ON purchase_line_items(purchase_id);
CREATE INDEX IF NOT EXISTS idx_purchase_line_items_product_id ON purchase_line_items(product_id);
CREATE INDEX IF NOT EXISTS idx_expenses_business_id ON expenses(business_id);
CREATE INDEX IF NOT EXISTS idx_expenses_incurred_at ON expenses(incurred_at);

-- +migrate Down
DROP TABLE IF EXISTS expenses;
DROP TABLE IF EXISTS purchase_line_items;
DROP TABLE IF EXISTS purchases;
DROP TABLE IF EXISTS suppliers;
ALTER TABLE sale_line_items DROP COLUMN IF EXISTS unit_cost;
ALTER TABLE products DROP COLUMN IF EXISTS cost_price;
