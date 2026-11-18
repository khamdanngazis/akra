-- Integritas ujian (T141): log kejadian per attempt, ringkasan di attempt
-- (jumlah keluar halaman, lama di luar, pindah perangkat), kunci sementara
-- sesuai aturan ujian (exams.integrity_mode/limit), dan izin pindah
-- perangkat dari pengawas.

ALTER TABLE exam_attempts
    ADD COLUMN leave_count         int         NOT NULL DEFAULT 0 CHECK (leave_count >= 0),
    ADD COLUMN away_ms             bigint      NOT NULL DEFAULT 0 CHECK (away_ms >= 0),
    ADD COLUMN device_moves        smallint    NOT NULL DEFAULT 0 CHECK (device_moves >= 0),
    -- Pengawas mengizinkan pindah: perangkat berikutnya yang membuka
    -- attempt menjadi perangkat aktif, token perangkat lama tidak berlaku.
    ADD COLUMN device_move_allowed boolean     NOT NULL DEFAULT false,
    -- Dikunci sementara (aturan kunci) sampai pengawas membuka.
    ADD COLUMN locked_at           timestamptz,
    -- leave_count saat terakhir dibuka: kunci berikutnya setelah N kali lagi.
    ADD COLUMN lock_base           int         NOT NULL DEFAULT 0 CHECK (lock_base >= 0);

CREATE TABLE exam_attempt_events (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id   uuid        NOT NULL,
    attempt_id  uuid        NOT NULL,
    kind        text        NOT NULL CHECK (kind IN ('keluar', 'layar_penuh', 'muat_ulang', 'salin', 'tempel', 'offline',
                                                     'perangkat_lain', 'pindah_perangkat', 'terkunci', 'dibuka')),
    occurred_at timestamptz NOT NULL,
    -- Lama kejadian (keluar halaman, offline), milidetik.
    duration_ms bigint      CHECK (duration_ms BETWEEN 0 AND 86400000),
    -- Nomor dari perangkat agar kirim ulang tidak tercatat dua kali.
    client_seq  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_attempt_events_attempt_fkey FOREIGN KEY (school_id, attempt_id) REFERENCES exam_attempts (school_id, id)
);
CREATE UNIQUE INDEX exam_attempt_events_seq_key ON exam_attempt_events (attempt_id, client_seq) WHERE client_seq IS NOT NULL;
CREATE INDEX exam_attempt_events_attempt_idx ON exam_attempt_events (attempt_id, id);
