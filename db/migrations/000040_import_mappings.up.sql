-- Mapping impor (T81): pemetaan judul kolom file sekolah ke kolom baku.
-- column_map = {"<judul kolom baku>": "<judul kolom di file>"}; isinya
-- divalidasi importer.ValidateMapping, database hanya menjaga bentuk.
CREATE TABLE import_mappings (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    name       text        NOT NULL,
    target     text        NOT NULL,
    column_map jsonb       NOT NULL,
    created_by uuid        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT import_mappings_target_check CHECK (target IN ('students', 'grades', 'fee_types')),
    CONSTRAINT import_mappings_name_check CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT import_mappings_map_check CHECK (jsonb_typeof(column_map) = 'object'),
    CONSTRAINT import_mappings_school_name_key UNIQUE (school_id, name),
    CONSTRAINT import_mappings_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT import_mappings_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
