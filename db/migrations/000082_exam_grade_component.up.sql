-- Kirim nilai ujian ke rapor (T150).

-- Komponen nilai rapor (T69) tujuan ujian ulangan/PTS/PAS. Beberapa ujian
-- boleh ke komponen yang sama (rata-rata, mis. UH1–UH4).
ALTER TABLE exams
    ADD COLUMN grade_component_id uuid,
    ADD CONSTRAINT exams_grade_component_fkey FOREIGN KEY (school_id, grade_component_id) REFERENCES grade_components (school_id, id);

-- Riwayat pengiriman ke rapor.
CREATE TABLE exam_grade_syncs (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL,
    exam_id      uuid        NOT NULL,
    component_id uuid        NOT NULL,
    -- timpa / tertinggi; remedial dan pengayaan mengikuti aturan T73/T137.
    policy       text        NOT NULL CHECK (policy IN ('timpa', 'tertinggi', 'remedial', 'pengayaan')),
    students     int         NOT NULL CHECK (students >= 0),
    changed      int         NOT NULL CHECK (changed >= 0),
    sent_by      uuid        NOT NULL,
    sent_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_grade_syncs_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_grade_syncs_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_grade_syncs_component_fkey FOREIGN KEY (school_id, component_id) REFERENCES grade_components (school_id, id),
    CONSTRAINT exam_grade_syncs_sent_by_fkey FOREIGN KEY (school_id, sent_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_grade_syncs_exam_idx ON exam_grade_syncs (exam_id, sent_at DESC);

-- Nilai ujian per siswa yang terakhir dikirim: dasar tanda "nilai berubah,
-- kirim ulang?" setelah nilai ulang atau koreksi esai.
CREATE TABLE exam_grade_sent (
    school_id        uuid    NOT NULL,
    exam_id          uuid    NOT NULL,
    student_id       uuid    NOT NULL,
    sync_id          uuid    NOT NULL,
    exam_score_x100  integer NOT NULL CHECK (exam_score_x100 BETWEEN 0 AND 10000),
    PRIMARY KEY (exam_id, student_id),
    CONSTRAINT exam_grade_sent_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_grade_sent_student_fkey FOREIGN KEY (school_id, student_id) REFERENCES students (school_id, id),
    CONSTRAINT exam_grade_sent_sync_fkey FOREIGN KEY (school_id, sync_id) REFERENCES exam_grade_syncs (school_id, id)
);
