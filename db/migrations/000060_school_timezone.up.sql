-- Zona waktu sekolah (T126). Jadwal dan "hari ini" mengikuti zona sekolah,
-- bukan jam server. Penyimpanan waktu tetap timestamptz (UTC).
ALTER TABLE schools
    ADD COLUMN timezone text NOT NULL DEFAULT 'Asia/Jakarta',
    ADD CONSTRAINT schools_timezone_check
        CHECK (timezone IN ('Asia/Jakarta', 'Asia/Makassar', 'Asia/Jayapura'));
