DELETE FROM school_settings WHERE key LIKE 'report.%';
ALTER TABLE school_settings DROP CONSTRAINT school_settings_key_check;
ALTER TABLE school_settings ADD CONSTRAINT school_settings_key_check CHECK (key ~ '^[a-z][a-z0-9_]*$');

ALTER TABLE subjects DROP CONSTRAINT IF EXISTS subjects_kkm_check;
ALTER TABLE subjects DROP COLUMN IF EXISTS kkm;
