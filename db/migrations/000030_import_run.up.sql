-- Penyimpanan hasil impor siswa (T55).
ALTER TABLE import_batches
    -- sync: dijalankan langsung (<= 50 baris valid); async: job River.
    ADD COLUMN mode          text,
    ADD COLUMN run_by        uuid,
    -- Tanggal penempatan kelas, ditetapkan saat "Simpan" agar percobaan
    -- ulang job memakai tanggal yang sama.
    ADD COLUMN run_on        date,
    ADD COLUMN river_job_id  bigint,
    ADD COLUMN started_at    timestamptz,
    ADD COLUMN created_count integer NOT NULL DEFAULT 0,
    ADD COLUMN updated_count integer NOT NULL DEFAULT 0,
    ADD COLUMN failed_count  integer NOT NULL DEFAULT 0,
    -- Pesan aman untuk pengguna; detail teknis hanya di log.
    ADD COLUMN error         text,
    ADD CONSTRAINT import_batches_mode_check CHECK (mode IN ('sync', 'async')),
    ADD CONSTRAINT import_batches_run_by_fkey FOREIGN KEY (school_id, run_by)
        REFERENCES users (school_id, id);

ALTER TABLE import_rows
    ADD COLUMN processed_at timestamptz,
    -- Hasil penyimpanan baris valid: baru, diperbarui, atau gagal.
    ADD COLUMN result       text,
    ADD COLUMN result_error text,
    ADD CONSTRAINT import_rows_result_check CHECK (
        (processed_at IS NULL AND result IS NULL)
        OR (processed_at IS NOT NULL AND status = 'valid' AND result IN ('baru', 'diperbarui', 'gagal')));

CREATE INDEX import_rows_pending_idx ON import_rows (batch_id, line) WHERE status = 'valid' AND processed_at IS NULL;
