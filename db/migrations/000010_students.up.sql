-- Data pribadi siswa. Keterikatan ke kelas ada di enrollments (T21).
CREATE TABLE students (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    -- Teks, bukan angka: NIS bisa berawalan nol ("007").
    nis        text        NOT NULL,
    full_name  text        NOT NULL CHECK (btrim(full_name) <> ''),
    birth_date date,
    gender     char(1),
    status     text        NOT NULL DEFAULT 'aktif',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT students_school_id_nis_key UNIQUE (school_id, nis),
    -- Target foreign key komposit untuk tabel yang merujuk siswa (enrollments).
    CONSTRAINT students_school_id_id_key UNIQUE (school_id, id),
    -- Service menormalisasi NIS (trim); constraint ini menjaga jika dilewati.
    CONSTRAINT students_nis_check CHECK (nis <> '' AND nis = btrim(nis)),
    CONSTRAINT students_gender_check CHECK (gender IN ('L', 'P') OR gender IS NULL),
    CONSTRAINT students_status_check CHECK (status IN ('aktif', 'pindah', 'lulus', 'keluar'))
);

CREATE INDEX students_school_id_full_name_idx ON students (school_id, full_name);
