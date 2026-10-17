-- Riwayat pembuatan tagihan massal. Proses kecil (<= 50 siswa) dijalankan
-- langsung; proses besar dijalankan job River dengan progres di sini.
CREATE TABLE bulk_invoice_runs (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL REFERENCES academic_years (id),
    fee_type_id      uuid        NOT NULL,
    class_id         uuid,       -- NULL berarti semua kelas
    period           text        NOT NULL CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    due_date         date        NOT NULL,
    amount           bigint      CHECK (amount >= 0), -- NULL: nominal default jenis tagihan
    mode             text        NOT NULL CHECK (mode IN ('sync', 'async')),
    status           text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    total_count      integer     NOT NULL DEFAULT 0,
    processed_count  integer     NOT NULL DEFAULT 0,
    created_count    integer     NOT NULL DEFAULT 0,
    skipped_count    integer     NOT NULL DEFAULT 0,
    error            text,
    river_job_id     bigint,
    requested_by     uuid        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    finished_at      timestamptz,
    CONSTRAINT bulk_invoice_runs_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT bulk_invoice_runs_fee_type_fkey FOREIGN KEY (school_id, fee_type_id)
        REFERENCES fee_types (school_id, id),
    CONSTRAINT bulk_invoice_runs_class_fkey FOREIGN KEY (school_id, academic_year_id, class_id)
        REFERENCES classes (school_id, academic_year_id, id),
    CONSTRAINT bulk_invoice_runs_requested_by_fkey FOREIGN KEY (school_id, requested_by)
        REFERENCES users (school_id, id)
);

CREATE INDEX bulk_invoice_runs_school_id_created_at_idx ON bulk_invoice_runs (school_id, created_at DESC);

-- Tagihan yang dibuat oleh proses massal. Jumlah "dibuat" dihitung dari sini,
-- sehingga tetap benar walau job diulang.
ALTER TABLE invoices ADD COLUMN bulk_run_id uuid;
ALTER TABLE invoices ADD CONSTRAINT invoices_bulk_run_fkey FOREIGN KEY (school_id, bulk_run_id)
    REFERENCES bulk_invoice_runs (school_id, id);
CREATE INDEX invoices_bulk_run_id_idx ON invoices (bulk_run_id) WHERE bulk_run_id IS NOT NULL;
