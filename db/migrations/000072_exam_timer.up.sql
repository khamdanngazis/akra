-- Timer ujian (T140): aturan terlambat masuk, waktu tambahan sesi, dan
-- jeda per sesi atau per peserta. Tenggat attempt (deadline_at) dihitung
-- ulang setiap kali jeda selesai atau waktu ditambah:
--   min(mulai + durasi + waktu tambahan peserta, akhir sesi)
--   + waktu tambahan sesi + total jeda
-- mulai = jam peserta mulai (late_policy 'penuh') atau jam mulai sesi
-- ('potong': keterlambatan memotong waktu mengerjakan).

ALTER TABLE exam_sessions
    ADD COLUMN late_policy   text        NOT NULL DEFAULT 'penuh' CHECK (late_policy IN ('penuh', 'potong')),
    -- Waktu tambahan untuk semua peserta sesi (mis. gangguan), menit.
    ADD COLUMN extra_minutes smallint    NOT NULL DEFAULT 0 CHECK (extra_minutes BETWEEN 0 AND 240),
    -- Sesi sedang dijeda sejak waktu ini (peserta baru tidak bisa mulai).
    ADD COLUMN paused_at     timestamptz;

ALTER TABLE exam_attempts
    -- Attempt sedang dijeda sejak waktu ini: layar terkunci, waktu berhenti.
    ADD COLUMN paused_at timestamptz,
    -- Total jeda yang sudah selesai (milidetik), masuk ke tenggat.
    ADD COLUMN paused_ms bigint NOT NULL DEFAULT 0 CHECK (paused_ms >= 0),
    ADD CONSTRAINT exam_attempts_paused_check CHECK (paused_at IS NULL OR status = 'berjalan');
