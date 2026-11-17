-- Ujian latihan bawaan (T136): paket latihan antarmuka tanpa mapel yang
-- otomatis dimiliki setiap sekolah.
ALTER TABLE exams DROP CONSTRAINT exams_subject_check;
ALTER TABLE exams ADD CONSTRAINT exams_subject_check CHECK (purpose IN ('seleksi', 'latihan') OR subject_id IS NOT NULL);
ALTER TABLE exams ADD COLUMN builtin boolean NOT NULL DEFAULT false;
-- Satu paket latihan bawaan aktif per sekolah.
CREATE UNIQUE INDEX exams_builtin_key ON exams (school_id) WHERE builtin AND archived_at IS NULL;

-- Nama route peserta di /u/... tidak boleh dipakai sebagai kode sekolah
-- (/u/{kode} adalah halaman login sekolah).
ALTER TABLE schools ADD CONSTRAINT schools_code_reserved CHECK (code NOT IN ('cek', 'beranda', 'keluar', 'ujian'));
