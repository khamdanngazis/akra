ALTER TABLE exams
    DROP CONSTRAINT exams_results_released_by_fkey,
    DROP CONSTRAINT exams_results_released_check,
    DROP COLUMN results_released_by,
    DROP COLUMN results_released_at;
