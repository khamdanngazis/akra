ALTER TABLE schools DROP CONSTRAINT schools_code_reserved;
DROP INDEX exams_builtin_key;
ALTER TABLE exams DROP COLUMN builtin;
ALTER TABLE exams DROP CONSTRAINT exams_subject_check;
ALTER TABLE exams ADD CONSTRAINT exams_subject_check CHECK (purpose = 'seleksi' OR subject_id IS NOT NULL);
