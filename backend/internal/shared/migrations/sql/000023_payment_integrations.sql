-- +migrate Up
CREATE TABLE IF NOT EXISTS mpesa_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    transaction_type TEXT NOT NULL CHECK (transaction_type IN ('stk_push', 'c2b', 'b2c')),
    merchant_request_id TEXT NOT NULL DEFAULT '',
    checkout_request_id TEXT NOT NULL DEFAULT '',
    mpesa_receipt_number TEXT NOT NULL DEFAULT '',
    phone_number TEXT NOT NULL DEFAULT '',
    amount BIGINT NOT NULL CHECK (amount >= 0),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'failed', 'cancelled')),
    result_code INTEGER,
    result_desc TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT,
    bill_ref_number TEXT NOT NULL DEFAULT '',
    account_reference TEXT NOT NULL DEFAULT '',
    reconciliation_status TEXT NOT NULL DEFAULT 'unmatched' CHECK (reconciliation_status IN ('reconciled', 'unmatched', 'ignored', 'manual')),
    reconciled_entity_type TEXT CHECK (reconciled_entity_type IS NULL OR reconciled_entity_type IN ('invoice', 'customer_credit', 'expense', 'supplier_payment')),
    reconciled_entity_id UUID,
    raw_payload TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_business_id ON mpesa_transactions(business_id);
CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_checkout_req ON mpesa_transactions(checkout_request_id);
CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_receipt ON mpesa_transactions(mpesa_receipt_number);
CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_reconciliation ON mpesa_transactions(business_id, reconciliation_status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mpesa_transactions_idempotency ON mpesa_transactions(business_id, idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE TABLE IF NOT EXISTS bank_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    bank_name TEXT NOT NULL DEFAULT '',
    reference TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('inflow', 'outflow')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    description TEXT NOT NULL DEFAULT '',
    transacted_at TIMESTAMPTZ NOT NULL,
    reconciliation_status TEXT NOT NULL DEFAULT 'unmatched' CHECK (reconciliation_status IN ('reconciled', 'unmatched', 'ignored', 'manual')),
    reconciled_entity_type TEXT CHECK (reconciled_entity_type IS NULL OR reconciled_entity_type IN ('invoice', 'customer_credit', 'expense', 'supplier_payment')),
    reconciled_entity_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_bank_transactions_business_reference UNIQUE (business_id, reference)
);

CREATE INDEX IF NOT EXISTS idx_bank_transactions_business_id ON bank_transactions(business_id);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_reconciliation ON bank_transactions(business_id, reconciliation_status);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_transacted_at ON bank_transactions(business_id, transacted_at);

-- +migrate Down
DROP TABLE IF EXISTS bank_transactions;
DROP TABLE IF EXISTS mpesa_transactions;
