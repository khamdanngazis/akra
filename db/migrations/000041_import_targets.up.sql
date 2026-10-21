-- Impor lewat mapping (T82): batch punya target (siswa atau nilai) dan
-- mapping yang dipakai saat unggah (NULL = judul kolom baku).
ALTER TABLE import_batches
    ADD COLUMN target     text NOT NULL DEFAULT 'students',
    ADD COLUMN mapping_id uuid,
    ADD CONSTRAINT import_batches_target_check CHECK (target IN ('students', 'grades')),
    ADD CONSTRAINT import_batches_mapping_fkey FOREIGN KEY (school_id, mapping_id)
        REFERENCES import_mappings (school_id, id);
