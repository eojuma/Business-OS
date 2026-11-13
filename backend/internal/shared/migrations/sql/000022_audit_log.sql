-- +migrate Up
-- Append-only activity trail. The application only inserts and reads these
-- rows; there are no update or delete endpoints.
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    actor_user_id UUID,
    actor_name TEXT NOT NULL DEFAULT '',
    actor_email TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id UUID,
    before JSONB,
    after JSONB,
    method TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 0,
    ip_address TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_business_created ON audit_logs(business_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_business_actor ON audit_logs(business_id, actor_user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_business_action ON audit_logs(business_id, action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_business_entity ON audit_logs(business_id, entity_type, entity_id);

-- +migrate Down
DROP TABLE IF EXISTS audit_logs;
