DROP TABLE IF EXISTS exam_regrades;
DROP TABLE IF EXISTS exam_key_overrides;
ALTER TABLE exam_attempts
    DROP CONSTRAINT IF EXISTS exam_attempts_score_check,
    DROP COLUMN IF EXISTS graded_at,
    DROP COLUMN IF EXISTS points_max,
    DROP COLUMN IF EXISTS points_earned_x10000,
    DROP COLUMN IF EXISTS score_status,
    DROP COLUMN IF EXISTS score_x100;
ALTER TABLE exam_answers DROP COLUMN IF EXISTS points_x10000;
