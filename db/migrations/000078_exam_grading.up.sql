-- Penilaian otomatis dan nilai ulang (T146).

-- Poin yang diperoleh per jawaban dalam satuan 1/10.000 poin (integer,
-- tanpa tipe pecahan): 2 poin = 20000, ⅔ poin = 6667. NULL: belum dinilai
-- (esai menunggu koreksi T147).
ALTER TABLE exam_answers ADD COLUMN points_x10000 integer CHECK (points_x10000 >= 0);

-- Nilai attempt: Σ poin / total poin × 100, disimpan sebagai integer
-- seperseratus (0–10000, mis. 8750 = 87,50) agar tidak ada selisih
-- pembulatan. menunggu_esai: ada esai yang belum dikoreksi; nilai ini
-- tidak dikirim ke modul lain (seleksi, rapor).
ALTER TABLE exam_attempts
    ADD COLUMN score_x100     integer CHECK (score_x100 BETWEEN 0 AND 10000),
    ADD COLUMN score_status   text CHECK (score_status IN ('final', 'menunggu_esai')),
    ADD COLUMN points_earned_x10000 integer,
    ADD COLUMN points_max     integer,
    ADD COLUMN graded_at      timestamptz,
    ADD CONSTRAINT exam_attempts_score_check CHECK ((score_status IS NULL) = (score_x100 IS NULL));

-- Kunci yang dibetulkan setelah ujian terkunci. Snapshot soal tidak bisa
-- diubah (append-only), jadi kunci koreksi disimpan di sini dan dipakai
-- penilaian menggantikan kunci snapshot. Riwayat lengkap di audit.
CREATE TABLE exam_key_overrides (
    school_id   uuid        NOT NULL,
    exam_id     uuid        NOT NULL,
    question_id uuid        NOT NULL,
    answer_key  jsonb       NOT NULL,
    reason      text        NOT NULL CHECK (btrim(reason) <> '' AND length(reason) <= 300),
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (exam_id, question_id),
    CONSTRAINT exam_key_overrides_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_key_overrides_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id),
    CONSTRAINT exam_key_overrides_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);

-- Riwayat nilai ulang per ujian (tampil di tab Hasil).
CREATE TABLE exam_regrades (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id   uuid        NOT NULL,
    exam_id     uuid        NOT NULL,
    question_id uuid,
    cause       text        NOT NULL CHECK (cause IN ('kunci', 'batal', 'pulih')),
    reason      text        NOT NULL DEFAULT '' CHECK (length(reason) <= 300),
    attempts    integer     NOT NULL,
    raised      integer     NOT NULL,
    lowered     integer     NOT NULL,
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_regrades_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_regrades_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_regrades_exam_idx ON exam_regrades (exam_id, id DESC);
