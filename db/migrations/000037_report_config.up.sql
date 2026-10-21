-- Pengaturan rapor (T73).

-- KKM per mapel. NULL berarti memakai report.kkm_default sekolah.
ALTER TABLE subjects ADD COLUMN kkm smallint;
ALTER TABLE subjects ADD CONSTRAINT subjects_kkm_check CHECK (kkm IS NULL OR kkm BETWEEN 0 AND 100);

-- Kunci pengaturan boleh bernamespace dengan titik, mis. report.scale.
ALTER TABLE school_settings DROP CONSTRAINT school_settings_key_check;
ALTER TABLE school_settings ADD CONSTRAINT school_settings_key_check
    CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$');
