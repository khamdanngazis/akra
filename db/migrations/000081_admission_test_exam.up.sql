-- Tes seleksi online (T149): tes gelombang bisa dikerjakan sebagai ujian
-- seleksi; nilainya masuk otomatis ke nilai tes pendaftar.

-- Cara tes: NULL = di kertas/wawancara (nilai diketik TU), terisi = ujian
-- online. Satu ujian untuk satu tes.
ALTER TABLE admission_tests
    ADD COLUMN exam_id uuid,
    ADD CONSTRAINT admission_tests_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    ADD CONSTRAINT admission_tests_exam_key UNIQUE (exam_id);

-- Sumber nilai: diketik TU, atau dari ujian online (tidak bisa disunting;
-- koreksi lewat nilai ulang ujian).
ALTER TABLE applicant_test_scores
    ADD COLUMN source     text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'ujian_online')),
    ADD COLUMN attempt_id uuid,
    ADD CONSTRAINT applicant_test_scores_attempt_fkey FOREIGN KEY (school_id, attempt_id) REFERENCES exam_attempts (school_id, id),
    ADD CONSTRAINT applicant_test_scores_attempt_check CHECK ((source = 'ujian_online') = (attempt_id IS NOT NULL));

-- Kode kartu pendaftar diturunkan dari kunci server dan nonce ini (HMAC),
-- sehingga kartu di halaman status bisa ditampilkan ulang tanpa menyimpan
-- kode; yang disimpan tetap hash-nya.
ALTER TABLE student_credentials ADD COLUMN code_nonce uuid;
