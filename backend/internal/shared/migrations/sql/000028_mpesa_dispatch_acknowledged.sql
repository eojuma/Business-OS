-- +migrate Up
-- A stored checkout/conversation id does not prove Daraja acknowledged the
-- dispatch: a 5xx can still include an id. Track acknowledgement separately so
-- replaying an ambiguous request keeps reporting uncertainty instead of
-- looking like a successful dispatch.
ALTER TABLE mpesa_transactions ADD COLUMN IF NOT EXISTS dispatch_acknowledged BOOLEAN NOT NULL DEFAULT FALSE;

-- +migrate Down
ALTER TABLE mpesa_transactions DROP COLUMN IF EXISTS dispatch_acknowledged;
