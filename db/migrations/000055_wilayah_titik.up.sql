-- Titik pusat (ibu kota) provinsi dan kabupaten/kota untuk memusatkan peta
-- pilih lokasi rumah (T112). Desa dan kecamatan tidak punya titik.
ALTER TABLE wilayah
    ADD COLUMN lat numeric(9,6),
    ADD COLUMN lng numeric(9,6),
    ADD CONSTRAINT wilayah_titik_check CHECK ((lat IS NULL) = (lng IS NULL));
