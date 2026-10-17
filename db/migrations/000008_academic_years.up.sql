CREATE TABLE academic_years (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    name       text        NOT NULL CHECK (btrim(name) <> ''),
    start_date date        NOT NULL,
    end_date   date        NOT NULL,
    -- Tahun ajaran baru selalu nonaktif; aktivasi dilakukan eksplisit oleh admin.
    is_active  boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT academic_years_dates_check CHECK (end_date > start_date),
    CONSTRAINT academic_years_school_id_name_key UNIQUE (school_id, name)
);

-- Tepat satu tahun ajaran aktif per sekolah.
CREATE UNIQUE INDEX academic_years_one_active ON academic_years (school_id) WHERE is_active;
