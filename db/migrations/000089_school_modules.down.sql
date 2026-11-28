-- Kembalikan status modul ujian ke kunci pengaturan lama sebelum tabel
-- dihapus.
INSERT INTO school_settings (school_id, key, value)
SELECT school_id, 'exam.enabled', CASE WHEN enabled THEN 'true' ELSE 'false' END FROM school_modules WHERE module = 'exam'
ON CONFLICT (school_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
INSERT INTO school_settings (school_id, key, value)
SELECT school_id, 'exam.release', options->>'release' FROM school_modules WHERE module = 'exam' AND options ? 'release'
ON CONFLICT (school_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
ALTER TABLE schools DROP COLUMN package;
DROP TABLE school_modules;
