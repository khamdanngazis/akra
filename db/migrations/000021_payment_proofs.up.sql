-- Bukti transfer (foto atau PDF) yang dilampirkan ke pembayaran. File
-- disimpan di object storage privat (R2); tabel ini hanya menyimpan kunci
-- dan sidik jarinya.
CREATE TABLE payment_proofs (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL REFERENCES schools (id),
    payment_id   uuid        NOT NULL,
    storage_key  text        NOT NULL,
    sha256       bytea       NOT NULL,
    size_bytes   integer     NOT NULL,
    content_type text        NOT NULL,
    uploaded_by  uuid        NOT NULL,
    uploaded_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payment_proofs_storage_key_key UNIQUE (storage_key),
    -- File yang sama tidak bisa dipakai untuk dua pembayaran di sekolah yang
    -- sama. Per sekolah (bukan global) agar sekolah lain tidak bisa mengetahui
    -- keberadaan file sekolah ini.
    CONSTRAINT payment_proofs_school_sha256_key UNIQUE (school_id, sha256),
    CONSTRAINT payment_proofs_sha256_check CHECK (octet_length(sha256) = 32),
    -- Maksimal 2 MB.
    CONSTRAINT payment_proofs_size_check CHECK (size_bytes > 0 AND size_bytes <= 2097152),
    CONSTRAINT payment_proofs_content_type_check CHECK (content_type IN ('image/jpeg', 'image/png', 'application/pdf')),
    CONSTRAINT payment_proofs_storage_key_check CHECK (storage_key LIKE 'schools/' || school_id::text || '/proofs/%'),
    CONSTRAINT payment_proofs_payment_fkey FOREIGN KEY (school_id, payment_id)
        REFERENCES payments (school_id, id),
    CONSTRAINT payment_proofs_uploaded_by_fkey FOREIGN KEY (school_id, uploaded_by)
        REFERENCES users (school_id, id),
    CONSTRAINT payment_proofs_school_id_id_key UNIQUE (school_id, id)
);

CREATE INDEX payment_proofs_payment_id_idx ON payment_proofs (payment_id);
