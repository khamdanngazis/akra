ALTER TABLE student_profiles
    DROP CONSTRAINT student_profiles_wilayah_check,
    DROP COLUMN wilayah_kode;
DROP TABLE wilayah_meta;
DROP TABLE wilayah;
