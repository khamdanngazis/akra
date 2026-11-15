-- Masa simpan data pendaftar yang tidak diterima (T113, UU PDP). Baris
-- pendaftar dianonimkan, bukan dihapus, agar rekap tetap benar.
ALTER TABLE applicants
    ADD COLUMN anonymized_at timestamptz,
    ADD COLUMN anonymized_reason text,
    ADD CONSTRAINT applicants_anonymized_check CHECK (
        (anonymized_at IS NULL) = (anonymized_reason IS NULL)
        AND (anonymized_reason IS NULL OR anonymized_reason IN ('masa_simpan', 'permintaan'))
    );
CREATE INDEX applicants_retention_idx ON applicants (period_id, status) WHERE anonymized_at IS NULL;
