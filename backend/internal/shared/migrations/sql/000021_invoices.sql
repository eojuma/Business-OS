-- +migrate Up
-- Per-business, per-year invoice numbering. The counter row is reserved in the
-- same transaction as the invoice insert so a rollback does not consume a
-- number.
CREATE TABLE IF NOT EXISTS invoice_number_counters (
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    year INTEGER NOT NULL,
    last_sequence BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (business_id, year)
);

CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    sale_id UUID NOT NULL UNIQUE REFERENCES sales(id) ON DELETE CASCADE,
    invoice_number TEXT NOT NULL,
    customer_id UUID REFERENCES customers(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'unpaid' CHECK (status IN ('unpaid', 'partial', 'paid')),
    issue_date TIMESTAMPTZ NOT NULL,
    due_date TIMESTAMPTZ NOT NULL,
    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),
    discount BIGINT NOT NULL DEFAULT 0 CHECK (discount >= 0),
    total_amount BIGINT NOT NULL CHECK (total_amount >= 0),
    amount_paid BIGINT NOT NULL DEFAULT 0 CHECK (amount_paid >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT invoices_amount_paid_within_total CHECK (amount_paid <= total_amount)
);

CREATE TABLE IF NOT EXISTS invoice_payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    amount BIGINT NOT NULL CHECK (amount > 0),
    note TEXT NOT NULL DEFAULT '',
    paid_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Invoice numbers are unique per business and year.
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_business_number ON invoices(business_id, invoice_number);
CREATE INDEX IF NOT EXISTS idx_invoices_business_status_due ON invoices(business_id, status, due_date);
CREATE INDEX IF NOT EXISTS idx_invoices_business_issue_date ON invoices(business_id, issue_date);
CREATE INDEX IF NOT EXISTS idx_invoices_customer_id ON invoices(customer_id);
CREATE INDEX IF NOT EXISTS idx_invoice_payments_invoice_id ON invoice_payments(invoice_id);
CREATE INDEX IF NOT EXISTS idx_invoice_payments_business_id ON invoice_payments(business_id);

-- +migrate Down
DROP TABLE IF EXISTS invoice_payments;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS invoice_number_counters;
