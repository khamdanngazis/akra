-- Layar pengawas (T143).

-- Terakhir perangkat menghubungi server (simpan atau ping). Ditulis paling
-- sering sekali per 30 detik per attempt (ADR 0001: detak tidak menulis ke
-- DB setiap kali). Dipakai untuk status offline di layar pengawas.
ALTER TABLE exam_attempts ADD COLUMN last_seen_at timestamptz;
UPDATE exam_attempts SET last_seen_at = coalesce(submitted_at, started_at);
ALTER TABLE exam_attempts ALTER COLUMN last_seen_at SET DEFAULT now(), ALTER COLUMN last_seen_at SET NOT NULL;

ALTER TABLE exam_sessions
    -- Token berganti tiap 15 menit; token sebelumnya masih diterima sebentar.
    ADD COLUMN token_prev        text CHECK (token_prev ~ '^[A-Z0-9]{4,8}$'),
    ADD COLUMN token_rotated_at  timestamptz,
    -- Pengawas menutup sesi: attempt berjalan dikumpulkan, tidak bisa mulai.
    ADD COLUMN closed_at         timestamptz,
    ADD COLUMN closed_by         uuid,
    -- Pengawas terakhir membuka layar pengawas (pusat kendali T144).
    ADD COLUMN proctor_seen_at   timestamptz,
    -- Berita acara: catatan pengawas dan tanda tangan (konfirmasi sandi).
    ADD COLUMN minutes_notes     text NOT NULL DEFAULT '' CHECK (length(minutes_notes) <= 4000),
    ADD COLUMN minutes_signed_at timestamptz,
    ADD COLUMN minutes_signed_by uuid,
    ADD CONSTRAINT exam_sessions_closed_by_fkey FOREIGN KEY (school_id, closed_by) REFERENCES users (school_id, id),
    ADD CONSTRAINT exam_sessions_minutes_signed_by_fkey FOREIGN KEY (school_id, minutes_signed_by) REFERENCES users (school_id, id),
    ADD CONSTRAINT exam_sessions_minutes_signed_check CHECK ((minutes_signed_at IS NULL) = (minutes_signed_by IS NULL));

-- Catatan kejadian dari pengawas (masuk berita acara). participant_id
-- kosong = catatan untuk sesi.
CREATE TABLE exam_session_notes (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id      uuid        NOT NULL,
    session_id     uuid        NOT NULL,
    participant_id uuid,
    body           text        NOT NULL CHECK (btrim(body) <> '' AND length(body) <= 500),
    created_by     uuid        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_session_notes_session_fkey FOREIGN KEY (school_id, session_id) REFERENCES exam_sessions (school_id, id),
    CONSTRAINT exam_session_notes_participant_fkey FOREIGN KEY (school_id, participant_id) REFERENCES exam_session_participants (school_id, id),
    CONSTRAINT exam_session_notes_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_session_notes_session_idx ON exam_session_notes (session_id, id);
