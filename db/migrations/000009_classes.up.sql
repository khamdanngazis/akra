-- Kunci unik (school_id, id) dibutuhkan sebagai target foreign key komposit,
-- sehingga database sendiri menjamin kelas, tahun ajaran, dan wali kelas
-- berasal dari sekolah yang sama.
ALTER TABLE academic_years ADD CONSTRAINT academic_years_school_id_id_key UNIQUE (school_id, id);
ALTER TABLE users ADD CONSTRAINT users_school_id_id_key UNIQUE (school_id, id);

CREATE TABLE classes (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid        NOT NULL REFERENCES schools (id),
    academic_year_id    uuid        NOT NULL,
    name                text        NOT NULL CHECK (btrim(name) <> ''),
    grade_level         smallint    NOT NULL,
    homeroom_teacher_id uuid,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT classes_grade_level_check CHECK (grade_level BETWEEN 1 AND 12),
    CONSTRAINT classes_academic_year_fkey FOREIGN KEY (school_id, academic_year_id)
        REFERENCES academic_years (school_id, id),
    -- MATCH SIMPLE (default): homeroom_teacher_id NULL tidak diperiksa.
    CONSTRAINT classes_homeroom_teacher_fkey FOREIGN KEY (school_id, homeroom_teacher_id)
        REFERENCES users (school_id, id)
);

-- Nama kelas unik per tahun ajaran, tanpa membedakan huruf besar/kecil
-- ("1A" dan "1a" dianggap sama).
CREATE UNIQUE INDEX classes_academic_year_id_name_key ON classes (academic_year_id, lower(name));
CREATE INDEX classes_school_id_academic_year_id_idx ON classes (school_id, academic_year_id);
CREATE INDEX classes_homeroom_teacher_id_idx ON classes (homeroom_teacher_id);
