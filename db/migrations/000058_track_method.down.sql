ALTER TABLE applicants DROP COLUMN IF EXISTS manual_rank, DROP COLUMN IF EXISTS track_score;
ALTER TABLE admission_tracks DROP COLUMN IF EXISTS score_max, DROP COLUMN IF EXISTS score_label, DROP COLUMN IF EXISTS method;
