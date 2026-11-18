ALTER TABLE exam_attempts
    DROP CONSTRAINT exam_attempts_paused_check,
    DROP COLUMN paused_ms,
    DROP COLUMN paused_at;

ALTER TABLE exam_sessions
    DROP COLUMN paused_at,
    DROP COLUMN extra_minutes,
    DROP COLUMN late_policy;
