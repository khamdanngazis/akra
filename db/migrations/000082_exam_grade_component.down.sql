DROP TABLE exam_grade_sent;
DROP TABLE exam_grade_syncs;
ALTER TABLE exams DROP CONSTRAINT exams_grade_component_fkey, DROP COLUMN grade_component_id;
