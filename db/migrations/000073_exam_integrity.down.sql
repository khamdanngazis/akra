DROP TABLE IF EXISTS exam_attempt_events;

ALTER TABLE exam_attempts
    DROP COLUMN lock_base,
    DROP COLUMN locked_at,
    DROP COLUMN device_move_allowed,
    DROP COLUMN device_moves,
    DROP COLUMN away_ms,
    DROP COLUMN leave_count;
