-- Nilai siswa per komponen penilaian (T70). score NULL berarti belum
-- dinilai, bukan nol. Satu nilai per enrollment per komponen.
ALTER TABLE grade_components ADD CONSTRAINT grade_components_school_year_id_key UNIQUE (school_id, academic_year_id, id);

CREATE TABLE grades (
    id               uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid         NOT NULL REFERENCES schools (id),
    academic_year_id uuid         NOT NULL,
    enrollment_id    uuid         NOT NULL,
    component_id     uuid         NOT NULL,
    score            numeric(5,2),
    recorded_by      uuid         NOT NULL,
    created_at       timestamptz  NOT NULL DEFAULT now(),
    updated_at       timestamptz  NOT NULL DEFAULT now(),
    updated_by       uuid,
    CONSTRAINT grades_score_check CHECK (score IS NULL OR (score >= 0 AND score <= 100)),
    CONSTRAINT grades_enrollment_component_key UNIQUE (enrollment_id, component_id),
    CONSTRAINT grades_school_id_id_key UNIQUE (school_id, id),
    -- Enrollment dan komponen harus dari sekolah dan tahun ajaran yang sama.
    CONSTRAINT grades_enrollment_fkey FOREIGN KEY (school_id, academic_year_id, enrollment_id)
        REFERENCES enrollments (school_id, academic_year_id, id),
    CONSTRAINT grades_component_fkey FOREIGN KEY (school_id, academic_year_id, component_id)
        REFERENCES grade_components (school_id, academic_year_id, id),
    CONSTRAINT grades_recorded_by_fkey FOREIGN KEY (school_id, recorded_by) REFERENCES users (school_id, id),
    CONSTRAINT grades_updated_by_fkey FOREIGN KEY (school_id, updated_by) REFERENCES users (school_id, id)
);

CREATE INDEX grades_component_id_idx ON grades (component_id);
