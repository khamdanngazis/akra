DELETE FROM report_jobs WHERE report_type = 'custom';
ALTER TABLE report_jobs DROP CONSTRAINT report_jobs_type_check;
ALTER TABLE report_jobs ADD CONSTRAINT report_jobs_type_check CHECK (report_type IN ('attendance', 'arrears'));
