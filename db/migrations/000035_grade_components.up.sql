-- Komponen penilaian per mapel per tahun ajaran (T69), mis. Tugas, UTS, UAS.
-- Total bobot komponen aktif per mapel harus 100; aturan itu dicek service
-- (bukan database) karena melibatkan beberapa baris.
CREATE TABLE grade_components (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    subject_id       uuid        NOT NULL,
    name             text        NOT NULL,
    weight_percent   smallint    NOT NULL,
    sort_order       smallint    NOT NULL DEFAULT 0,
    is_active        boolean     NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT grade_components_weight_check CHECK (weight_percent BETWEEN 1 AND 100),
    CONSTRAINT grade_components_name_check CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 40),
    CONSTRAINT grade_components_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT grade_components_year_fkey FOREIGN KEY (school_id, academic_year_id)
        REFERENCES academic_years (school_id, id),
    CONSTRAINT grade_components_subject_fkey FOREIGN KEY (school_id, subject_id)
        REFERENCES subjects (school_id, id)
);

-- Nama unik per mapel per tahun ajaran tanpa membedakan huruf besar/kecil
-- ("UTS" = "uts").
CREATE UNIQUE INDEX grade_components_subject_year_name_key
    ON grade_components (subject_id, academic_year_id, lower(name));
