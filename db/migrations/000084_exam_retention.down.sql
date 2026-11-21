DROP INDEX exam_sessions_retention_idx;
-- Peserta yang sudah dianonimkan tidak bisa dikembalikan; baris itu
-- dihapus beserta attempt-nya agar constraint lama berlaku lagi.
DELETE FROM applicant_test_scores WHERE attempt_id IN (SELECT t.id FROM exam_attempts t
    JOIN exam_session_participants p ON p.id = t.participant_id WHERE p.anonymized_at IS NOT NULL);
DELETE FROM exam_attempts WHERE participant_id IN (SELECT id FROM exam_session_participants WHERE anonymized_at IS NOT NULL);
DELETE FROM exam_session_participants WHERE anonymized_at IS NOT NULL;
ALTER TABLE exam_session_participants
    DROP CONSTRAINT exam_session_participants_owner_check,
    ADD CONSTRAINT exam_session_participants_owner_check CHECK (num_nonnulls(student_id, applicant_id) = 1),
    DROP COLUMN anonymized_at;
ALTER TABLE exam_sessions DROP COLUMN data_anonymized_at;
