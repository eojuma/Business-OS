-- +migrate Up
-- New rows must default to unverified. 000018 added the column with DEFAULT
-- TRUE so existing accounts were grandfathered; that default must not apply to
-- future inserts.
ALTER TABLE users ALTER COLUMN email_verified SET DEFAULT FALSE;

-- +migrate Down
ALTER TABLE users ALTER COLUMN email_verified SET DEFAULT TRUE;
