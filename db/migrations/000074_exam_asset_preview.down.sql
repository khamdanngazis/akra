ALTER TABLE exam_assets
    DROP CONSTRAINT exam_assets_preview_check,
    DROP COLUMN preview_bytes,
    DROP COLUMN preview_type,
    DROP COLUMN preview_key;
