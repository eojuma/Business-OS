-- +migrate Up
-- A transient "claiming" reconciliation state lets manual reconciliation
-- serialize per transaction without colliding with the terminal "manual"
-- dismissal state. A stale claim is released back to "unmatched".
ALTER TABLE mpesa_transactions DROP CONSTRAINT IF EXISTS mpesa_transactions_reconciliation_status_check;
ALTER TABLE mpesa_transactions ADD CONSTRAINT mpesa_transactions_reconciliation_status_check
    CHECK (reconciliation_status IN ('reconciled', 'unmatched', 'ignored', 'manual', 'claiming'));

ALTER TABLE bank_transactions DROP CONSTRAINT IF EXISTS bank_transactions_reconciliation_status_check;
ALTER TABLE bank_transactions ADD CONSTRAINT bank_transactions_reconciliation_status_check
    CHECK (reconciliation_status IN ('reconciled', 'unmatched', 'ignored', 'manual', 'claiming'));

-- +migrate Down
-- Any in-flight claim is abandoned; return it to the reviewable state first.
UPDATE mpesa_transactions SET reconciliation_status = 'unmatched' WHERE reconciliation_status = 'claiming';
UPDATE bank_transactions SET reconciliation_status = 'unmatched' WHERE reconciliation_status = 'claiming';

ALTER TABLE mpesa_transactions DROP CONSTRAINT IF EXISTS mpesa_transactions_reconciliation_status_check;
ALTER TABLE mpesa_transactions ADD CONSTRAINT mpesa_transactions_reconciliation_status_check
    CHECK (reconciliation_status IN ('reconciled', 'unmatched', 'ignored', 'manual'));

ALTER TABLE bank_transactions DROP CONSTRAINT IF EXISTS bank_transactions_reconciliation_status_check;
ALTER TABLE bank_transactions ADD CONSTRAINT bank_transactions_reconciliation_status_check
    CHECK (reconciliation_status IN ('reconciled', 'unmatched', 'ignored', 'manual'));
