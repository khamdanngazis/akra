-- Telaah soal (T132, opsional per sekolah lewat pengaturan exam.review_enabled):
-- draf → ditelaah → siap. Penelaah (guru mapel sama, koordinator, admin_tu,
-- bukan pembuat) menyetujui atau meminta perbaikan dengan catatan.
CREATE TABLE exam_question_reviews (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL,
    question_id uuid        NOT NULL,
    version     int         NOT NULL,
    reviewer_id uuid        NOT NULL,
    decision    text        NOT NULL CHECK (decision IN ('setuju', 'perbaiki')),
    note        text        NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_question_reviews_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id),
    CONSTRAINT exam_question_reviews_reviewer_fkey FOREIGN KEY (school_id, reviewer_id) REFERENCES users (school_id, id)
);
CREATE INDEX exam_question_reviews_question_idx ON exam_question_reviews (question_id, created_at DESC);
