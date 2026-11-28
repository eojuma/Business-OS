-- +migrate Up
-- Reconciliation claims need an owner so a stale attempt cannot release or
-- finalize a claim that a newer attempt has already taken. The transient
-- "claiming" status from 000026 remains the serialization point; this column
-- identifies which attempt currently owns it.
ALTER TABLE mpesa_transactions ADD COLUMN IF NOT EXISTS reconciliation_claim_id UUID;
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS reconciliation_claim_id UUID;

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_claim ON mpesa_transactions(reconciliation_claim_id) WHERE reconciliation_claim_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_bank_transactions_claim ON bank_transactions(reconciliation_claim_id) WHERE reconciliation_claim_id IS NOT NULL;

-- +migrate Down
DROP INDEX IF EXISTS idx_bank_transactions_claim;
DROP INDEX IF EXISTS idx_mpesa_transactions_claim;

ALTER TABLE bank_transactions DROP COLUMN IF EXISTS reconciliation_claim_id;
ALTER TABLE mpesa_transactions DROP COLUMN IF EXISTS reconciliation_claim_id;
