-- Pembayaran tagihan. Baris tidak pernah diubah atau dihapus; pembatalan
-- (T38) dicatat di tabel terpisah. Nominal dalam rupiah (bilangan bulat).
CREATE TABLE payments (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid        NOT NULL REFERENCES schools (id),
    invoice_id     uuid        NOT NULL,
    amount         bigint      NOT NULL,
    paid_on        date        NOT NULL,
    method         text        NOT NULL,
    -- Rekening tujuan, wajib untuk transfer.
    bank_account   text,
    -- Nomor referensi transfer.
    reference      text,
    note           text,
    -- Diisi saat pembayaran dicatat, dalam transaksi yang sama (T41).
    receipt_number text,
    recorded_by    uuid        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payments_amount_check CHECK (amount > 0),
    CONSTRAINT payments_method_check CHECK (method IN ('tunai', 'transfer')),
    CONSTRAINT payments_bank_account_check CHECK (method = 'tunai' OR (bank_account IS NOT NULL AND btrim(bank_account) <> '')),
    CONSTRAINT payments_invoice_fkey FOREIGN KEY (school_id, invoice_id)
        REFERENCES invoices (school_id, id),
    CONSTRAINT payments_recorded_by_fkey FOREIGN KEY (school_id, recorded_by)
        REFERENCES users (school_id, id),
    CONSTRAINT payments_receipt_number_key UNIQUE (school_id, receipt_number),
    -- Target foreign key komposit pembatalan (T38).
    CONSTRAINT payments_school_id_id_key UNIQUE (school_id, id)
);

CREATE INDEX payments_invoice_id_idx ON payments (invoice_id);
CREATE INDEX payments_school_paid_on_idx ON payments (school_id, paid_on);

-- Sama dengan audit_logs (000007): trigger menolak UPDATE, DELETE, dan
-- TRUNCATE untuk semua user, termasuk pemilik tabel, karena aplikasi dan
-- migrasi memakai user database yang sama.
CREATE FUNCTION payments_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payments is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER payments_no_update_delete
    BEFORE UPDATE OR DELETE ON payments
    FOR EACH ROW EXECUTE FUNCTION payments_append_only();

CREATE TRIGGER payments_no_truncate
    BEFORE TRUNCATE ON payments
    FOR EACH STATEMENT EXECUTE FUNCTION payments_append_only();

REVOKE UPDATE, DELETE, TRUNCATE ON payments FROM PUBLIC;
