ALTER TABLE wilayah
    DROP CONSTRAINT wilayah_titik_check,
    DROP COLUMN lng,
    DROP COLUMN lat;
-- Paksa impor ulang data wilayah tanpa titik saat server start berikutnya.
DELETE FROM wilayah_meta;
