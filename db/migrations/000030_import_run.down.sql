DROP INDEX IF EXISTS import_rows_pending_idx;

ALTER TABLE import_rows
    DROP CONSTRAINT IF EXISTS import_rows_result_check,
    DROP COLUMN IF EXISTS result_error,
    DROP COLUMN IF EXISTS result,
    DROP COLUMN IF EXISTS processed_at;

ALTER TABLE import_batches
    DROP CONSTRAINT IF EXISTS import_batches_run_by_fkey,
    DROP CONSTRAINT IF EXISTS import_batches_mode_check,
    DROP COLUMN IF EXISTS error,
    DROP COLUMN IF EXISTS failed_count,
    DROP COLUMN IF EXISTS updated_count,
    DROP COLUMN IF EXISTS created_count,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS river_job_id,
    DROP COLUMN IF EXISTS run_on,
    DROP COLUMN IF EXISTS run_by,
    DROP COLUMN IF EXISTS mode;
