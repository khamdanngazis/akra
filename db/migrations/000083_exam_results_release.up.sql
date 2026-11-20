-- Hasil untuk siswa dan orang tua (T151): guru membuka hasil setelah semua
-- sesi ujian (termasuk susulan) berakhir. Selama terbuka, kode kartu siswa
-- berlaku 30 hari untuk melihat hasil (student_credentials.results_until).
ALTER TABLE exams
    ADD COLUMN results_released_at timestamptz,
    ADD COLUMN results_released_by uuid,
    ADD CONSTRAINT exams_results_released_check CHECK ((results_released_at IS NULL) = (results_released_by IS NULL)),
    ADD CONSTRAINT exams_results_released_by_fkey FOREIGN KEY (school_id, results_released_by) REFERENCES users (school_id, id);
