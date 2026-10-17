DROP TABLE IF EXISTS classes;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_school_id_id_key;
ALTER TABLE academic_years DROP CONSTRAINT IF EXISTS academic_years_school_id_id_key;
