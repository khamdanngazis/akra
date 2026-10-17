-- Orang tua/wali. Satu siswa bisa punya beberapa wali, dan satu wali bisa
-- punya beberapa anak di sekolah yang sama.
CREATE TABLE guardians (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    full_name  text        NOT NULL CHECK (btrim(full_name) <> ''),
    -- Dinormalisasi di service ke satu format: +62xxxxxxxx.
    phone      text        CHECK (phone ~ '^\+[0-9]{8,15}$'),
    -- Huruf kecil, dinormalisasi di service. Wajib untuk akun portal (T24).
    email      text        CHECK (email = lower(email) AND email <> ''),
    address    text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT guardians_school_id_id_key UNIQUE (school_id, id)
);

CREATE INDEX guardians_school_id_full_name_idx ON guardians (school_id, full_name);

-- school_id ikut disimpan agar foreign key komposit menjamin siswa dan wali
-- berasal dari sekolah yang sama.
CREATE TABLE student_guardians (
    school_id    uuid    NOT NULL REFERENCES schools (id),
    student_id   uuid    NOT NULL,
    guardian_id  uuid    NOT NULL,
    relationship text    NOT NULL,
    is_primary   boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (student_id, guardian_id),
    CONSTRAINT student_guardians_student_fkey FOREIGN KEY (school_id, student_id)
        REFERENCES students (school_id, id),
    CONSTRAINT student_guardians_guardian_fkey FOREIGN KEY (school_id, guardian_id)
        REFERENCES guardians (school_id, id),
    CONSTRAINT student_guardians_relationship_check CHECK (relationship IN ('ayah', 'ibu', 'wali', 'lainnya'))
);

-- Satu penanggung jawab utama per siswa.
CREATE UNIQUE INDEX student_one_primary ON student_guardians (student_id) WHERE is_primary;
CREATE INDEX student_guardians_guardian_id_idx ON student_guardians (guardian_id);
