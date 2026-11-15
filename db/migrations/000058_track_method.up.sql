-- Metode seleksi per jalur (T118). uses_test dipertahankan selaras dengan
-- method = 'tes' agar kode lama tetap benar.
ALTER TABLE admission_tracks
    ADD COLUMN method text NOT NULL DEFAULT 'urutan'
        CONSTRAINT admission_tracks_method_check CHECK (method IN ('urutan', 'tes', 'nilai', 'manual')),
    ADD COLUMN score_label text,
    ADD COLUMN score_max integer CONSTRAINT admission_tracks_score_max_check CHECK (score_max BETWEEN 1 AND 10000);
UPDATE admission_tracks SET method = 'tes' WHERE uses_test;

-- Nilai jalur (dalam perseratus, mis. 8550 = 85,50) diisi calon siswa untuk
-- jalur metode nilai; urutan manual diisi TU untuk jalur metode manual.
ALTER TABLE applicants
    ADD COLUMN track_score integer CONSTRAINT applicants_track_score_check CHECK (track_score >= 0),
    ADD COLUMN manual_rank integer CONSTRAINT applicants_manual_rank_check CHECK (manual_rank >= 1);
