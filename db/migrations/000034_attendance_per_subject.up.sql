-- Absensi per mapel (T68). Sesi dengan subject_id NULL adalah absensi
-- harian; sesi dengan subject_id adalah absensi satu mata pelajaran.
ALTER TABLE attendance_sessions ADD COLUMN subject_id uuid;

ALTER TABLE attendance_sessions ADD CONSTRAINT attendance_sessions_subject_fkey
    FOREIGN KEY (school_id, subject_id) REFERENCES subjects (school_id, id);

-- Satu sesi per kelas, tanggal, dan mapel; sesi harian dihitung sebagai satu
-- "mapel" kosong. Unique index (bukan constraint) karena memakai ekspresi.
ALTER TABLE attendance_sessions DROP CONSTRAINT attendance_sessions_class_date_key;
CREATE UNIQUE INDEX attendance_sessions_class_date_subject_key
    ON attendance_sessions (class_id, session_date, coalesce(subject_id, '00000000-0000-0000-0000-000000000000'::uuid));
