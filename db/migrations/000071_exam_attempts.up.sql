-- Pengerjaan ujian oleh peserta (T138). Satu attempt = satu kali mengerjakan:
-- varian, urutan soal dan opsi hasil acak disimpan agar muat ulang
-- menampilkan susunan yang sama. Simpan jawaban berbasis token dan offline
-- menyusul di T139; tenggat lengkap (jeda, terlambat) di T140.

-- Token sesi (dibacakan pengawas, T143). Bawaan: tidak diwajibkan.
ALTER TABLE exam_sessions
    ADD COLUMN token_required boolean NOT NULL DEFAULT false,
    ADD COLUMN token          text CHECK (token ~ '^[A-Z0-9]{4,8}$');

CREATE TABLE exam_attempts (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL,
    exam_id          uuid        NOT NULL,
    session_id       uuid        NOT NULL,
    participant_id   uuid        NOT NULL,
    attempt_no       smallint    NOT NULL DEFAULT 1 CHECK (attempt_no BETWEEN 1 AND 10),
    variant_id       uuid        NOT NULL,
    seed             bigint      NOT NULL,
    -- Urutan soal (question_id) dan opsi per soal hasil acak.
    question_order   uuid[]      NOT NULL,
    option_order     jsonb       NOT NULL DEFAULT '{}',
    status           text        NOT NULL DEFAULT 'berjalan' CHECK (status IN ('berjalan', 'dikumpulkan')),
    device_id        uuid        NOT NULL,
    started_at       timestamptz NOT NULL,
    deadline_at      timestamptz NOT NULL,
    submitted_at     timestamptz,
    -- Siapa yang mengumpulkan: peserta, waktu habis, atau pengawas/sistem.
    submit_reason    text        CHECK (submit_reason IN ('peserta', 'waktu', 'pengawas', 'sistem')),
    -- Soal terakhir yang dibuka (indeks di question_order).
    current_position int         NOT NULL DEFAULT 0 CHECK (current_position >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_attempts_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_attempts_no_key UNIQUE (participant_id, attempt_no),
    CONSTRAINT exam_attempts_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_attempts_session_fkey FOREIGN KEY (school_id, session_id) REFERENCES exam_sessions (school_id, id),
    CONSTRAINT exam_attempts_participant_fkey FOREIGN KEY (school_id, participant_id) REFERENCES exam_session_participants (school_id, id),
    CONSTRAINT exam_attempts_variant_fkey FOREIGN KEY (school_id, variant_id) REFERENCES exam_variants (school_id, id),
    CONSTRAINT exam_attempts_submitted_check CHECK ((status = 'dikumpulkan') = (submitted_at IS NOT NULL AND submit_reason IS NOT NULL))
);
-- Paling banyak satu attempt berjalan per peserta sesi.
CREATE UNIQUE INDEX exam_attempts_running_key ON exam_attempts (participant_id) WHERE status = 'berjalan';
CREATE INDEX exam_attempts_session_idx ON exam_attempts (session_id);
CREATE INDEX exam_attempts_deadline_idx ON exam_attempts (deadline_at) WHERE status = 'berjalan';

-- Jawaban terakhir per soal. client_seq dari perangkat: perubahan dengan
-- seq lebih kecil diabaikan agar request terlambat tidak menimpa jawaban
-- yang lebih baru. value null = belum dijawab (mis. hanya ditandai ragu).
CREATE TABLE exam_answers (
    school_id   uuid        NOT NULL,
    attempt_id  uuid        NOT NULL,
    question_id uuid        NOT NULL,
    value       jsonb       NOT NULL DEFAULT 'null',
    flagged     boolean     NOT NULL DEFAULT false,
    client_seq  bigint      NOT NULL,
    saved_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, question_id),
    CONSTRAINT exam_answers_attempt_fkey FOREIGN KEY (school_id, attempt_id) REFERENCES exam_attempts (school_id, id)
);
