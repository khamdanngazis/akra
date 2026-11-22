-- Konten contoh modul ujian (T154): bank contoh per jenjang ditandai agar
-- tampil berlabel "Contoh" dan bisa dibedakan dari bank sekolah.
ALTER TABLE exam_banks ADD COLUMN sample boolean NOT NULL DEFAULT false;
