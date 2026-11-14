-- Biaya formulir pendaftaran (T94). Sengaja terpisah dari invoices: tagihan
-- terikat enrollment siswa, sedangkan pendaftar belum menjadi siswa.
--
-- fee_amount di-snapshot dari periode saat pendaftaran pertama kali dikirim,
-- agar perubahan biaya periode tidak mengubah tagihan yang sudah ada.
ALTER TABLE applicants ADD COLUMN fee_amount bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT applicants_fee_check CHECK (fee_amount >= 0);

-- Pembayaran biaya formulir. Harus lunas penuh (tidak ada cicilan). Satu
-- pembayaran aktif per pendaftar; yang dibatalkan tetap tersimpan.
-- Nomor kuitansi memakai seri yang sama dengan pembayaran siswa (KW/...).
CREATE TABLE applicant_fee_payments (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid        NOT NULL REFERENCES schools (id),
    applicant_id   uuid        NOT NULL,
    amount         bigint      NOT NULL,
    paid_on        date        NOT NULL,
    method         text        NOT NULL,
    bank_account   text,
    reference      text,
    receipt_number text        NOT NULL,
    recorded_by    uuid        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    voided_at      timestamptz,
    voided_by      uuid,
    void_reason    text,
    CONSTRAINT applicant_fee_payments_applicant_fkey FOREIGN KEY (school_id, applicant_id) REFERENCES applicants (school_id, id),
    CONSTRAINT applicant_fee_payments_recorded_by_fkey FOREIGN KEY (school_id, recorded_by) REFERENCES users (school_id, id),
    CONSTRAINT applicant_fee_payments_voided_by_fkey FOREIGN KEY (school_id, voided_by) REFERENCES users (school_id, id),
    CONSTRAINT applicant_fee_payments_amount_check CHECK (amount > 0),
    CONSTRAINT applicant_fee_payments_method_check CHECK (method IN ('tunai', 'transfer')),
    CONSTRAINT applicant_fee_payments_bank_check CHECK (method = 'tunai' OR (bank_account IS NOT NULL AND btrim(bank_account) <> '')),
    CONSTRAINT applicant_fee_payments_void_check CHECK ((voided_at IS NULL) = (voided_by IS NULL) AND (voided_at IS NULL) = (void_reason IS NULL)),
    CONSTRAINT applicant_fee_payments_receipt_key UNIQUE (school_id, receipt_number)
);
CREATE UNIQUE INDEX applicant_fee_payments_one_active ON applicant_fee_payments (applicant_id) WHERE voided_at IS NULL;
