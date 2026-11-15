-- Acuan tes seleksi (T119): rubrik/kriteria dan nilai minimal lulus.
ALTER TABLE admission_tests
    ADD COLUMN rubric text CONSTRAINT admission_tests_rubric_check CHECK (char_length(rubric) <= 2000),
    ADD COLUMN pass_score integer CONSTRAINT admission_tests_pass_score_check CHECK (pass_score >= 0);
