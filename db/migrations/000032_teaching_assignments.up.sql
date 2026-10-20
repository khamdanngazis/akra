-- Penugasan guru mata pelajaran per kelas (T65). Satu mapel di satu kelas
-- hanya punya satu guru. Foreign key komposit dengan school_id menjamin
-- kelas, mapel, dan guru berasal dari sekolah yang sama, dan kelas memang
-- milik tahun ajaran yang dicatat.
CREATE TABLE teaching_assignments (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    class_id         uuid        NOT NULL,
    subject_id       uuid        NOT NULL,
    teacher_id       uuid        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT teaching_assignments_class_subject_key UNIQUE (class_id, subject_id),
    CONSTRAINT teaching_assignments_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT teaching_assignments_class_fkey FOREIGN KEY (school_id, academic_year_id, class_id)
        REFERENCES classes (school_id, academic_year_id, id),
    CONSTRAINT teaching_assignments_subject_fkey FOREIGN KEY (school_id, subject_id)
        REFERENCES subjects (school_id, id),
    CONSTRAINT teaching_assignments_teacher_fkey FOREIGN KEY (school_id, teacher_id)
        REFERENCES users (school_id, id)
);

-- "Kelas yang saya ajar" (absensi, jadwal) dan pengecekan akses guru.
CREATE INDEX teaching_assignments_teacher_idx ON teaching_assignments (school_id, teacher_id, class_id);
