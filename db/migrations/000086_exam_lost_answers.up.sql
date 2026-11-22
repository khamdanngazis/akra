-- Jawaban yang hilang (T155): perubahan jawaban yang dikirim perangkat
-- setelah attempt ditutup (lewat tenggat dan tenggang) dan belum pernah
-- tersimpan di server. Dasar ukuran "0 jawaban hilang" saat pilot, dan
-- salinan agar jawaban bisa ditinjau atau dipulihkan sekolah. Satu baris per
-- soal; kirim ulang dari perangkat menyimpan versi terbaru saja.
CREATE TABLE exam_lost_answers (
    school_id   uuid        NOT NULL,
    attempt_id  uuid        NOT NULL,
    question_id uuid        NOT NULL,
    value       jsonb       NOT NULL,
    client_seq  bigint      NOT NULL,
    reported_at timestamptz NOT NULL,
    PRIMARY KEY (attempt_id, question_id),
    CONSTRAINT exam_lost_answers_attempt_fkey FOREIGN KEY (school_id, attempt_id) REFERENCES exam_attempts (school_id, id)
);
CREATE INDEX exam_lost_answers_school_idx ON exam_lost_answers (school_id, reported_at);
