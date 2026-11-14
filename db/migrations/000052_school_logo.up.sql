-- Logo sekolah untuk halaman pendaftaran publik (T111). File disimpan di
-- storage (tidak terenkripsi karena memang tampil publik); logo_key memuat
-- akhiran acak sehingga URL berubah setiap logo diganti.
ALTER TABLE schools
    ADD COLUMN logo_key text,
    ADD COLUMN logo_type text,
    ADD CONSTRAINT schools_logo_check CHECK (
        (logo_key IS NULL) = (logo_type IS NULL)
        AND (logo_type IS NULL OR logo_type IN ('image/png', 'image/jpeg'))
    );
