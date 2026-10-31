-- +migrate Up
-- Single-use, expiring tokens for verifying an email address. Only the SHA-256
-- hash of the token is stored, so a database leak cannot be replayed.
CREATE TABLE IF NOT EXISTS email_verification_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_email_verification_tokens_hash
    ON email_verification_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_email_verification_tokens_user
    ON email_verification_tokens(user_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_email_verification_tokens_user;
DROP INDEX IF EXISTS idx_email_verification_tokens_hash;
DROP TABLE IF EXISTS email_verification_tokens;
