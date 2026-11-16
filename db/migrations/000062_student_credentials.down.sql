DELETE FROM rate_limit_attempts WHERE kind NOT IN ('draft', 'resume', 'status');
ALTER INDEX rate_limit_attempts_key_idx RENAME TO admission_attempts_key_idx;
ALTER INDEX rate_limit_attempts_ip_idx RENAME TO admission_attempts_ip_idx;
ALTER TABLE rate_limit_attempts DROP CONSTRAINT rate_limit_attempts_kind_check;
ALTER TABLE rate_limit_attempts ADD CONSTRAINT admission_attempts_kind_check CHECK (kind IN ('draft', 'resume', 'status'));
ALTER TABLE rate_limit_attempts RENAME TO admission_attempts;
DROP TABLE student_credentials;
