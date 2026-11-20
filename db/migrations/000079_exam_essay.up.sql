-- Koreksi esai (T147).

-- Skor esai tetap di points_x10000 (T146); di sini komentar untuk siswa
-- dan siapa/kapan mengoreksi.
ALTER TABLE exam_answers
    ADD COLUMN essay_comment   text NOT NULL DEFAULT '' CHECK (length(essay_comment) <= 1000),
    ADD COLUMN essay_graded_by uuid,
    ADD COLUMN essay_graded_at timestamptz,
    ADD CONSTRAINT exam_answers_essay_graded_check CHECK ((essay_graded_by IS NULL) = (essay_graded_at IS NULL)),
    ADD CONSTRAINT exam_answers_essay_graded_by_fkey FOREIGN KEY (school_id, essay_graded_by) REFERENCES users (school_id, id);

-- Penugasan koreksi: guru lain mengoreksi satu soal esai (atau semua) untuk
-- satu sesi/ruang (atau semua). Hanya memberi akses koreksi, bukan akses
-- ujian.
CREATE TABLE exam_grader_assignments (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id   uuid        NOT NULL,
    exam_id     uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    question_id uuid,
    session_id  uuid,
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_grader_assignments_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_grader_assignments_user_fkey FOREIGN KEY (school_id, user_id) REFERENCES users (school_id, id),
    CONSTRAINT exam_grader_assignments_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id),
    CONSTRAINT exam_grader_assignments_session_fkey FOREIGN KEY (school_id, session_id) REFERENCES exam_sessions (school_id, id),
    CONSTRAINT exam_grader_assignments_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT exam_grader_assignments_key UNIQUE NULLS NOT DISTINCT (exam_id, user_id, question_id, session_id)
);
CREATE INDEX exam_grader_assignments_user_idx ON exam_grader_assignments (school_id, user_id);
