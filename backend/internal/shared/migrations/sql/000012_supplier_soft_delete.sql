-- +migrate Up
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- +migrate Down
ALTER TABLE suppliers DROP COLUMN IF EXISTS deleted_at;
