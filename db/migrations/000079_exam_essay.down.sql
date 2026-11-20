DROP TABLE IF EXISTS exam_grader_assignments;
ALTER TABLE exam_answers
    DROP CONSTRAINT IF EXISTS exam_answers_essay_graded_by_fkey,
    DROP CONSTRAINT IF EXISTS exam_answers_essay_graded_check,
    DROP COLUMN IF EXISTS essay_graded_at,
    DROP COLUMN IF EXISTS essay_graded_by,
    DROP COLUMN IF EXISTS essay_comment;
