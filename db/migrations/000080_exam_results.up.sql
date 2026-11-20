-- Hasil dan analisis ujian (T148).

-- Nilai manual untuk koreksi khusus (mis. kendala teknis): poin di
-- points_x10000 tetap dipakai saat nilai ulang selama override_by terisi.
ALTER TABLE exam_answers
    ADD COLUMN override_by     uuid,
    ADD COLUMN override_at     timestamptz,
    ADD COLUMN override_reason text CHECK (length(override_reason) <= 500),
    ADD CONSTRAINT exam_answers_override_check CHECK (
        (override_by IS NULL) = (override_at IS NULL) AND (override_by IS NULL) = (override_reason IS NULL)),
    ADD CONSTRAINT exam_answers_override_points_check CHECK (override_by IS NULL OR points_x10000 IS NOT NULL),
    ADD CONSTRAINT exam_answers_override_by_fkey FOREIGN KEY (school_id, override_by) REFERENCES users (school_id, id);

-- Statistik butir: satu baris per soal per ujian (diperbarui saat analisis
-- dibuka ulang).
CREATE UNIQUE INDEX exam_question_stats_exam_question_key ON exam_question_stats (exam_id, question_id);
