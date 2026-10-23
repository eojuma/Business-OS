-- +migrate Up
-- +migrate NoTransaction

-- Emails are resolved and stored case-insensitively (see auth.normalizeEmail).
-- Lowercase existing rows first so the unique index below can enforce that
-- invariant. If legacy rows differ only by case, this UPDATE aborts with a
-- unique violation on users_email_key and the migration stops before the
-- index is created; resolve the duplicate manually (merge or rename) and
-- re-run, because silently picking one account would be data loss.
UPDATE users
SET email = LOWER(TRIM(email))
WHERE email <> LOWER(TRIM(email));

-- CREATE INDEX CONCURRENTLY cannot run inside a transaction, hence the
-- NoTransaction marker. The index makes the LOWER(email) lookup in
-- FindByEmail deterministic and guarantees at most one match.
--
-- A failed concurrent build can leave an invalid index behind; IF NOT EXISTS
-- would then skip the rebuild on retry and record the migration as applied
-- without enforcing uniqueness. Drop invalid leftovers first.
DO $$
BEGIN
    IF to_regclass('idx_users_email_lower') IS NOT NULL AND EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_users_email_lower') AND NOT indisvalid
    ) THEN
        DROP INDEX idx_users_email_lower;
    END IF;
END
$$;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_users_email_lower ON users (LOWER(email));

-- +migrate Down
-- Plain DROP INDEX: Down always runs inside a transaction, where the
-- CONCURRENTLY variant is not allowed.
DROP INDEX IF EXISTS idx_users_email_lower;
