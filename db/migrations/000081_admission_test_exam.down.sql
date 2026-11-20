ALTER TABLE student_credentials DROP COLUMN code_nonce;
DELETE FROM applicant_test_scores WHERE source = 'ujian_online';
ALTER TABLE applicant_test_scores
    DROP CONSTRAINT applicant_test_scores_attempt_check,
    DROP CONSTRAINT applicant_test_scores_attempt_fkey,
    DROP COLUMN attempt_id,
    DROP COLUMN source;
ALTER TABLE admission_tests
    DROP CONSTRAINT admission_tests_exam_key,
    DROP CONSTRAINT admission_tests_exam_fkey,
    DROP COLUMN exam_id;
