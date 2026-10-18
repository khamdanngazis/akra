-- Ekspor laporan besar yang dijalankan sebagai job (T49). File hasil ada di
-- storage privat (file_key) dan dihapus setelah 7 hari (file_deleted_at).
CREATE TABLE report_jobs (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    requested_by    uuid        NOT NULL,
    report_type     text        NOT NULL,
    format          text        NOT NULL,
    params          jsonb       NOT NULL DEFAULT '{}',
    status          text        NOT NULL DEFAULT 'antrean',
    row_count       integer,
    file_key        text,
    file_name       text,
    -- Pesan aman untuk pengguna; detail teknis hanya di log.
    error           text,
    river_job_id    bigint,
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    file_deleted_at timestamptz,
    CONSTRAINT report_jobs_type_check CHECK (report_type IN ('attendance', 'arrears')),
    CONSTRAINT report_jobs_format_check CHECK (format IN ('xlsx', 'pdf')),
    CONSTRAINT report_jobs_status_check CHECK (status IN ('antrean', 'berjalan', 'selesai', 'gagal')),
    CONSTRAINT report_jobs_file_key_check CHECK (file_key IS NULL OR file_key LIKE 'schools/' || school_id::text || '/reports/%'),
    CONSTRAINT report_jobs_requested_by_fkey FOREIGN KEY (school_id, requested_by)
        REFERENCES users (school_id, id)
);

CREATE INDEX report_jobs_school_created_idx ON report_jobs (school_id, created_at DESC);
CREATE INDEX report_jobs_cleanup_idx ON report_jobs (finished_at) WHERE file_key IS NOT NULL;
