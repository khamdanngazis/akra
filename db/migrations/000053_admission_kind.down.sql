DELETE FROM applicant_documents WHERE doc_type IN ('surat_pindah', 'rapor');
ALTER TABLE applicant_documents
    DROP CONSTRAINT applicant_documents_type_check,
    ADD CONSTRAINT applicant_documents_type_check
        CHECK (doc_type IN ('kk', 'akta', 'ijazah', 'foto', 'kip', 'lainnya'));

DELETE FROM admission_counters WHERE kind = 'pindahan';
ALTER TABLE admission_counters
    DROP CONSTRAINT admission_counters_pkey,
    ADD PRIMARY KEY (school_id, academic_year_id),
    DROP CONSTRAINT admission_counters_kind_check,
    DROP COLUMN kind;

ALTER TABLE applicants
    DROP CONSTRAINT applicants_period_kind_fkey,
    DROP CONSTRAINT applicants_transfer_check,
    DROP CONSTRAINT applicants_kind_check,
    DROP COLUMN transfer,
    DROP COLUMN kind;

UPDATE admission_periods SET target_grade = 1 WHERE target_grade IS NULL;
UPDATE admission_periods SET required_docs = array_remove(array_remove(required_docs, 'surat_pindah'), 'rapor');
ALTER TABLE admission_periods
    DROP CONSTRAINT admission_periods_docs_check,
    ADD CONSTRAINT admission_periods_docs_check CHECK (required_docs <@ ARRAY['kk', 'akta', 'ijazah', 'foto', 'kip']),
    ALTER COLUMN target_grade SET NOT NULL,
    DROP CONSTRAINT admission_periods_kind_grade_check,
    DROP CONSTRAINT admission_periods_school_id_kind_key,
    DROP CONSTRAINT admission_periods_kind_check,
    DROP COLUMN kind;
