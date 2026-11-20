DROP INDEX exam_question_stats_exam_question_key;
ALTER TABLE exam_answers
    DROP CONSTRAINT exam_answers_override_by_fkey,
    DROP CONSTRAINT exam_answers_override_points_check,
    DROP CONSTRAINT exam_answers_override_check,
    DROP COLUMN override_reason,
    DROP COLUMN override_at,
    DROP COLUMN override_by;
