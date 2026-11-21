-- Masa simpan data ujian (T153, UU PDP): setelah akhir tahun ajaran
-- berikutnya, jawaban, kejadian, dan kode akses dihapus; peserta dilepas
-- dari identitas siswa/pendaftar. Nilai dan durasi tetap (tanpa identitas)
-- untuk statistik; nilai rapor mengikuti aturan rapor.
ALTER TABLE exam_sessions ADD COLUMN data_anonymized_at timestamptz;
ALTER TABLE exam_session_participants
    ADD COLUMN anonymized_at timestamptz,
    DROP CONSTRAINT exam_session_participants_owner_check,
    ADD CONSTRAINT exam_session_participants_owner_check CHECK (
        num_nonnulls(student_id, applicant_id) = 1
        OR (anonymized_at IS NOT NULL AND student_id IS NULL AND applicant_id IS NULL));
CREATE INDEX exam_sessions_retention_idx ON exam_sessions (ends_at) WHERE data_anonymized_at IS NULL;
