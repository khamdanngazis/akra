DROP INDEX applicants_retention_idx;
ALTER TABLE applicants
    DROP CONSTRAINT applicants_anonymized_check,
    DROP COLUMN anonymized_reason,
    DROP COLUMN anonymized_at;
