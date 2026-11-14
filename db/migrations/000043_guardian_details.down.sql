DROP INDEX student_one_biological_mother;
DROP INDEX student_one_biological_father;
ALTER TABLE student_guardians DROP COLUMN is_biological;
DROP INDEX guardians_school_nik_key;
ALTER TABLE guardians
    DROP CONSTRAINT guardians_special_needs_check,
    DROP CONSTRAINT guardians_ref_check,
    DROP CONSTRAINT guardians_birth_year_check,
    DROP CONSTRAINT guardians_nik_pair_check,
    DROP COLUMN special_needs,
    DROP COLUMN income,
    DROP COLUMN occupation,
    DROP COLUMN education,
    DROP COLUMN birth_year,
    DROP COLUMN nik_idx,
    DROP COLUMN nik_enc;
