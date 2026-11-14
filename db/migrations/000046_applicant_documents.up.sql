-- Dokumen persyaratan pendaftar (T91). Isi file dienkripsi aplikasi
-- (AES-256-GCM, kunci PII T85b) sebelum disimpan di storage, karena scan KK
-- dan akta memuat NIK. File dibuka lewat handler yang mendekripsi, bukan
-- URL presign. sha256 dihitung dari isi asli (sebelum enkripsi).
CREATE TABLE applicant_documents (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL REFERENCES schools (id),
    applicant_id uuid        NOT NULL,
    doc_type     text        NOT NULL,
    storage_key  text        NOT NULL,
    content_type text        NOT NULL,
    size_bytes   integer     NOT NULL,
    sha256       bytea       NOT NULL,
    uploaded_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT applicant_documents_applicant_fkey FOREIGN KEY (school_id, applicant_id) REFERENCES applicants (school_id, id),
    CONSTRAINT applicant_documents_type_check CHECK (doc_type IN ('kk', 'akta', 'ijazah', 'foto', 'kip', 'lainnya')),
    CONSTRAINT applicant_documents_content_type_check CHECK (content_type IN ('image/jpeg', 'image/png', 'application/pdf')),
    CONSTRAINT applicant_documents_size_check CHECK (size_bytes BETWEEN 1 AND 2097152),
    CONSTRAINT applicant_documents_one_per_type UNIQUE (applicant_id, doc_type),
    CONSTRAINT applicant_documents_key_key UNIQUE (storage_key)
);
