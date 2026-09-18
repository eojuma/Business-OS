-- +migrate Up
ALTER TABLE sales ADD COLUMN IF NOT EXISTS sale_type TEXT NOT NULL DEFAULT 'cash'
    CHECK (sale_type IN ('cash', 'credit', 'quotation'));
CREATE INDEX IF NOT EXISTS idx_sales_sale_type ON sales(sale_type);

CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warning', 'critical')),
    recipient TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    entity_id UUID,
    entity_name TEXT NOT NULL DEFAULT '',
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notifications_business_id ON notifications(business_id);
CREATE INDEX IF NOT EXISTS idx_notifications_business_read ON notifications(business_id, is_read);
CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at);
CREATE INDEX IF NOT EXISTS idx_notifications_entity ON notifications(type, entity_id);

-- +migrate Down
DROP TABLE IF EXISTS notifications;
DROP INDEX IF EXISTS idx_sales_sale_type;
ALTER TABLE sales DROP COLUMN IF EXISTS sale_type;
