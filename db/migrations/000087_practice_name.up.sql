-- Nama paket latihan bawaan (T159): "Coba Halaman Ujian", agar tidak
-- tertukar dengan jenis ujian "Ujian latihan" yang dibuat guru. Hanya paket
-- bawaan yang namanya belum diubah sekolah.
UPDATE exams SET name = 'Coba Halaman Ujian', updated_at = now() WHERE builtin AND name = 'Latihan Antarmuka Ujian';
