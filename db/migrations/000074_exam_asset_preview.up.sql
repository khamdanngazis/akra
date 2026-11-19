-- Pratinjau ringan gambar soal untuk halaman ujian (T142, hemat data):
-- sisi terpanjang paling besar 960 px. Gambar penuh hanya dimuat saat
-- peserta memperbesar gambar. preview_key NULL = belum dibuat (gambar lama
-- dibuat saat pertama diminta); sama dengan storage_key bila gambar asli
-- sudah cukup kecil.
ALTER TABLE exam_assets
    ADD COLUMN preview_key   text,
    ADD COLUMN preview_type  text CHECK (preview_type IN ('image/png', 'image/jpeg', 'image/webp', 'image/gif')),
    ADD COLUMN preview_bytes int  CHECK (preview_bytes > 0),
    ADD CONSTRAINT exam_assets_preview_check CHECK ((preview_key IS NULL) = (preview_type IS NULL) AND (preview_key IS NULL) = (preview_bytes IS NULL));
