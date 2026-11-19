-- Pusat kendali koordinator (T144).

-- Pergeseran tenggat (ms) agar sisa waktu peserta tetap sama saat dipindah
-- ke sesi lain (aturan waktu sesi tujuan bisa berbeda). Bisa negatif.
ALTER TABLE exam_attempts ADD COLUMN shift_ms bigint NOT NULL DEFAULT 0;

-- Pesan koordinator ke layar peserta: satu baris per sesi tujuan. Dikirim
-- ke perangkat lewat balasan simpan/ping (id lebih besar dari yang terakhir
-- diterima perangkat). Juga dicatat di exam_session_notes (berita acara).
CREATE TABLE exam_broadcasts (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id  uuid        NOT NULL,
    session_id uuid        NOT NULL,
    body       text        NOT NULL CHECK (btrim(body) <> '' AND length(body) <= 300),
    created_by uuid        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_broadcasts_session_fkey FOREIGN KEY (school_id, session_id) REFERENCES exam_sessions (school_id, id),
    CONSTRAINT exam_broadcasts_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_broadcasts_session_idx ON exam_broadcasts (session_id, id);

-- Soal yang dibatalkan saat ujian: poinnya dikeluarkan dari total semua
-- peserta (dipakai penilaian T146). Kunci yang salah dibetulkan lewat nilai
-- ulang setelah ujian.
CREATE TABLE exam_voided_questions (
    school_id   uuid        NOT NULL,
    exam_id     uuid        NOT NULL,
    question_id uuid        NOT NULL,
    reason      text        NOT NULL DEFAULT '' CHECK (length(reason) <= 300),
    voided_by   uuid        NOT NULL,
    voided_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (exam_id, question_id),
    CONSTRAINT exam_voided_questions_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_voided_questions_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id),
    CONSTRAINT exam_voided_questions_voided_by_fkey FOREIGN KEY (school_id, voided_by) REFERENCES users (school_id, id)
);
