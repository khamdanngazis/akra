-- Data wilayah administrasi Indonesia (T110), global untuk semua sekolah.
-- Diisi otomatis oleh aplikasi dari data yang disertakan
-- (internal/wilayah/data); kode yang hilang di pembaruan Kepmendagri
-- berikutnya ditandai tidak aktif, bukan dihapus, karena mungkin masih
-- dipakai data siswa lama.
CREATE TABLE wilayah (
    kode     text     PRIMARY KEY,
    nama     text     NOT NULL,
    tingkat  smallint NOT NULL,
    induk    text,
    kode_pos text,
    aktif    boolean  NOT NULL DEFAULT true,
    CONSTRAINT wilayah_kode_check CHECK (kode ~ '^[0-9]{2}(\.[0-9]{2}(\.[0-9]{2}(\.[0-9]{4})?)?)?$'),
    CONSTRAINT wilayah_tingkat_check CHECK (tingkat BETWEEN 1 AND 4),
    CONSTRAINT wilayah_induk_check CHECK ((tingkat = 1) = (induk IS NULL)),
    CONSTRAINT wilayah_kode_pos_check CHECK (kode_pos IS NULL OR kode_pos ~ '^[0-9]{5}$')
);
CREATE INDEX wilayah_induk_idx ON wilayah (induk, nama);
CREATE INDEX wilayah_nama_idx ON wilayah (tingkat, lower(nama));

CREATE TABLE wilayah_meta (
    id          boolean     PRIMARY KEY DEFAULT true CHECK (id),
    versi       text        NOT NULL,
    jumlah      integer     NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now()
);

-- Kode desa/kelurahan di data rinci siswa. Nama kecamatan dan desa tetap
-- disimpan di kolom district/village (dipakai lembar salin dan ekspor
-- Dapodik). Kosong berarti diisi manual atau data lama.
ALTER TABLE student_profiles
    ADD COLUMN wilayah_kode text,
    ADD CONSTRAINT student_profiles_wilayah_check CHECK (wilayah_kode IS NULL OR wilayah_kode ~ '^[0-9]{2}\.[0-9]{2}\.[0-9]{2}\.[0-9]{4}$');
