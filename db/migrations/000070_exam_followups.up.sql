-- Susulan, remedial, dan pengayaan (T137). Susulan adalah sesi
-- (kind = 'susulan') di ujian yang sama, sehingga nilainya satu kolom.
-- Remedial dan pengayaan adalah ujian baru yang menunjuk ujian induk.
ALTER TABLE exams
    ADD COLUMN parent_exam_id uuid,
    ADD COLUMN followup text CHECK (followup IN ('remedial', 'pengayaan')),
    ADD CONSTRAINT exams_parent_fkey FOREIGN KEY (school_id, parent_exam_id) REFERENCES exams (school_id, id),
    ADD CONSTRAINT exams_followup_parent_check CHECK ((parent_exam_id IS NULL) = (followup IS NULL));
CREATE INDEX exams_parent_idx ON exams (parent_exam_id) WHERE parent_exam_id IS NOT NULL;

-- Remedial terfokus: TP yang belum tuntas per peserta. Halaman ujian
-- (T138) hanya menampilkan bagian TP ini. Kosong = semua bagian.
ALTER TABLE exam_session_participants ADD COLUMN focus_objectives uuid[] NOT NULL DEFAULT '{}';
