DROP TABLE IF EXISTS exam_answers;
DROP TABLE IF EXISTS exam_attempts;
ALTER TABLE exam_sessions DROP COLUMN IF EXISTS token, DROP COLUMN IF EXISTS token_required;
