DROP TABLE IF EXISTS exam_voided_questions;
DROP TABLE IF EXISTS exam_broadcasts;
ALTER TABLE exam_attempts DROP COLUMN IF EXISTS shift_ms;
