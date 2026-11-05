-- +migrate Up
-- Email/password sign-ups must verify ownership of the address before they can
-- sign in. Existing accounts predate verification and are trusted, so they are
-- grandfathered as verified to avoid locking anyone out. New sign-ups set this
-- to false explicitly in the application.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT TRUE;

-- +migrate Down
ALTER TABLE users DROP COLUMN IF EXISTS email_verified;
