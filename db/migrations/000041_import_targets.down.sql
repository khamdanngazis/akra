DELETE FROM import_batches WHERE target <> 'students';
ALTER TABLE import_batches
    DROP CONSTRAINT IF EXISTS import_batches_mapping_fkey,
    DROP CONSTRAINT IF EXISTS import_batches_target_check,
    DROP COLUMN IF EXISTS mapping_id,
    DROP COLUMN IF EXISTS target;
