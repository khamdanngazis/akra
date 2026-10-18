-- Hasil validasi per baris impor siswa (T54). Validasi tidak menulis ke
-- students, guardians, atau enrollments.
-- Kunci (school_id, id) agar import_rows hanya bisa menunjuk batch sekolah
-- yang sama.
ALTER TABLE import_batches
    ADD CONSTRAINT import_batches_school_id_key UNIQUE (school_id, id),
    ADD COLUMN update_rows integer;

CREATE TABLE import_rows (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL,
    batch_id   uuid        NOT NULL,
    -- Nomor baris di Excel (bukan indeks internal).
    line       integer     NOT NULL,
    -- Isi sel apa adanya, urutan kolom importer.StudentColumns.
    raw        jsonb       NOT NULL DEFAULT '[]',
    status     text        NOT NULL,
    -- Untuk baris valid: 'baru' atau 'diperbarui' (NIS sudah ada).
    action     text,
    -- Data yang sudah dinormalisasi, dipakai saat menyimpan (T55).
    data       jsonb,
    -- [{"column": "Kelas", "kind": "kelas_tidak_ada", "message": "..."}]
    errors     jsonb       NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT import_rows_status_check CHECK (status IN ('valid', 'error', 'dilewati')),
    CONSTRAINT import_rows_action_check CHECK (
        (status = 'valid' AND action IN ('baru', 'diperbarui') AND data IS NOT NULL)
        OR (status <> 'valid' AND action IS NULL)),
    CONSTRAINT import_rows_line_check CHECK (line >= 1),
    CONSTRAINT import_rows_batch_fkey FOREIGN KEY (school_id, batch_id)
        REFERENCES import_batches (school_id, id) ON DELETE CASCADE,
    CONSTRAINT import_rows_batch_line_key UNIQUE (batch_id, line)
);

CREATE INDEX import_rows_batch_status_idx ON import_rows (batch_id, status, line);
