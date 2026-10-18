DROP TABLE IF EXISTS import_rows;

ALTER TABLE import_batches
    DROP COLUMN IF EXISTS update_rows,
    DROP CONSTRAINT IF EXISTS import_batches_school_id_key;
