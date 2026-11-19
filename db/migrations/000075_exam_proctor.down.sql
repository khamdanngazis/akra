DROP TABLE IF EXISTS exam_session_notes;

ALTER TABLE exam_sessions
    DROP CONSTRAINT exam_sessions_minutes_signed_check,
    DROP CONSTRAINT exam_sessions_minutes_signed_by_fkey,
    DROP CONSTRAINT exam_sessions_closed_by_fkey,
    DROP COLUMN minutes_signed_by,
    DROP COLUMN minutes_signed_at,
    DROP COLUMN minutes_notes,
    DROP COLUMN proctor_seen_at,
    DROP COLUMN closed_by,
    DROP COLUMN closed_at,
    DROP COLUMN token_rotated_at,
    DROP COLUMN token_prev;

ALTER TABLE exam_attempts DROP COLUMN last_seen_at;
