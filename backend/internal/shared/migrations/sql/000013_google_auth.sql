-- +migrate Up
-- Google-only accounts have no local password, so password_hash must be
-- nullable. google_id stores the Google subject ("sub") and is unique when set.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS google_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_id ON users (google_id) WHERE google_id IS NOT NULL;

-- +migrate Down
DROP INDEX IF EXISTS idx_users_google_id;
ALTER TABLE users DROP COLUMN IF EXISTS google_id;
-- Give Google-only users an unusable password hash before restoring NOT NULL.
UPDATE users SET password_hash = '' WHERE password_hash IS NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
