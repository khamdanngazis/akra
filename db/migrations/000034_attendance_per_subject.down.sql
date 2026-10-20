-- Menghapus absensi per mapel. Sesi per mapel beserta catatannya ikut
-- terhapus, karena constraint lama hanya mengizinkan satu sesi per kelas
-- per tanggal.
DELETE FROM attendance_records r USING attendance_sessions s
WHERE r.school_id = s.school_id AND r.session_id = s.id AND s.subject_id IS NOT NULL;
DELETE FROM attendance_sessions WHERE subject_id IS NOT NULL;

DROP INDEX IF EXISTS attendance_sessions_class_date_subject_key;
ALTER TABLE attendance_sessions ADD CONSTRAINT attendance_sessions_class_date_key UNIQUE (class_id, session_date);
ALTER TABLE attendance_sessions DROP CONSTRAINT IF EXISTS attendance_sessions_subject_fkey;
ALTER TABLE attendance_sessions DROP COLUMN IF EXISTS subject_id;
