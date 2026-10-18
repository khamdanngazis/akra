-- Batch impor siswa (T53, kolom mengikuti rancangan T55). Hasil parsing
-- disimpan sementara di parsed_rows sampai divalidasi (T54); file aslinya
-- tidak disimpan.
CREATE TABLE import_batches (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid        NOT NULL REFERENCES schools (id),
    uploaded_by    uuid        NOT NULL,
    file_name      text        NOT NULL,
    status         text        NOT NULL DEFAULT 'diunggah',
    total_rows     integer     NOT NULL,
    skipped_rows   integer     NOT NULL DEFAULT 0,
    valid_rows     integer,
    error_rows     integer,
    processed_rows integer     NOT NULL DEFAULT 0,
    -- [{"line": 3, "values": ["NIS", "Nama", ...]}], urutan kolom importer.StudentColumns.
    parsed_rows    jsonb,
    -- [{"line": 5, "reason": "kosong"}]
    skipped        jsonb       NOT NULL DEFAULT '[]',
    created_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    CONSTRAINT import_batches_status_check CHECK (status IN ('diunggah', 'divalidasi', 'berjalan', 'selesai', 'gagal')),
    CONSTRAINT import_batches_rows_check CHECK (total_rows >= 0 AND skipped_rows >= 0 AND processed_rows >= 0),
    CONSTRAINT import_batches_file_name_check CHECK (char_length(file_name) BETWEEN 1 AND 255),
    CONSTRAINT import_batches_uploaded_by_fkey FOREIGN KEY (school_id, uploaded_by)
        REFERENCES users (school_id, id)
);

CREATE INDEX import_batches_school_created_idx ON import_batches (school_id, created_at DESC);
