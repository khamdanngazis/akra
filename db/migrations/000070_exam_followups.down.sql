ALTER TABLE exam_session_participants DROP COLUMN focus_objectives;
DROP INDEX exams_parent_idx;
ALTER TABLE exams
    DROP CONSTRAINT exams_followup_parent_check,
    DROP CONSTRAINT exams_parent_fkey,
    DROP COLUMN followup,
    DROP COLUMN parent_exam_id;
