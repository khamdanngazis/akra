-- Konfigurasi laporan kustom tersimpan (T77). Isinya divalidasi
-- builder.Compile (T76) sebelum disimpan; database hanya menjaga bentuk.
CREATE TABLE report_configs (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid        NOT NULL REFERENCES schools (id),
    owner_user_id uuid        NOT NULL,
    name          text        NOT NULL,
    source        text        NOT NULL,
    columns       jsonb       NOT NULL,
    filters       jsonb       NOT NULL DEFAULT '[]',
    sort          jsonb       NOT NULL DEFAULT '[]',
    is_shared     boolean     NOT NULL DEFAULT false,
    -- Hapus = soft delete: deleted_at diisi dan is_shared dimatikan.
    deleted_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT report_configs_owner_fkey FOREIGN KEY (school_id, owner_user_id) REFERENCES users (school_id, id),
    CONSTRAINT report_configs_name_check CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT report_configs_json_check CHECK (jsonb_typeof(columns) = 'array' AND jsonb_typeof(filters) = 'array' AND jsonb_typeof(sort) = 'array'),
    CONSTRAINT report_configs_deleted_not_shared CHECK (deleted_at IS NULL OR NOT is_shared),
    CONSTRAINT report_configs_school_id_id_key UNIQUE (school_id, id)
);

-- Nama unik per pemilik di antara konfigurasi yang belum dihapus (tanpa
-- membedakan huruf besar/kecil); nama konfigurasi terhapus boleh dipakai lagi.
CREATE UNIQUE INDEX report_configs_owner_name_key
    ON report_configs (school_id, owner_user_id, lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX report_configs_school_shared_idx ON report_configs (school_id) WHERE is_shared;
