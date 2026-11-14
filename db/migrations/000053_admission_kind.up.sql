-- Formulir siswa baru dan siswa pindahan dipisah (T109). Jenis ditentukan
-- periode; pendaftar menyalin jenis periodenya (dijaga FK) agar mudah
-- difilter. Data khusus pindahan disimpan di applicants.transfer.
-- Periode pindahan tidak terikat satu tingkat: tingkat tujuan dipilih
-- pendaftar.
ALTER TABLE admission_periods
    ADD COLUMN kind text NOT NULL DEFAULT 'baru',
    ALTER COLUMN target_grade DROP NOT NULL,
    ADD CONSTRAINT admission_periods_kind_check CHECK (kind IN ('baru', 'pindahan')),
    ADD CONSTRAINT admission_periods_kind_grade_check CHECK (kind = 'pindahan' OR target_grade IS NOT NULL),
    DROP CONSTRAINT admission_periods_docs_check,
    ADD CONSTRAINT admission_periods_docs_check
        CHECK (required_docs <@ ARRAY['kk', 'akta', 'ijazah', 'foto', 'kip', 'surat_pindah', 'rapor']),
    ADD CONSTRAINT admission_periods_school_id_kind_key UNIQUE (school_id, id, kind);

ALTER TABLE applicants
    ADD COLUMN kind text NOT NULL DEFAULT 'baru',
    ADD COLUMN transfer jsonb NOT NULL DEFAULT '{}',
    ADD CONSTRAINT applicants_kind_check CHECK (kind IN ('baru', 'pindahan')),
    ADD CONSTRAINT applicants_transfer_check CHECK (jsonb_typeof(transfer) = 'object'),
    ADD CONSTRAINT applicants_period_kind_fkey FOREIGN KEY (school_id, period_id, kind)
        REFERENCES admission_periods (school_id, id, kind);

-- Nomor pendaftaran pindahan memakai urutan sendiri (PINDAH-2027-0001).
ALTER TABLE admission_counters
    ADD COLUMN kind text NOT NULL DEFAULT 'baru',
    ADD CONSTRAINT admission_counters_kind_check CHECK (kind IN ('baru', 'pindahan')),
    DROP CONSTRAINT admission_counters_pkey,
    ADD PRIMARY KEY (school_id, academic_year_id, kind);

ALTER TABLE applicant_documents
    DROP CONSTRAINT applicant_documents_type_check,
    ADD CONSTRAINT applicant_documents_type_check
        CHECK (doc_type IN ('kk', 'akta', 'ijazah', 'foto', 'kip', 'surat_pindah', 'rapor', 'lainnya'));
