-- Mata pelajaran (T64). Dipakai penugasan guru (T65), jadwal (T66), dan nilai.
-- Mapel tidak dihapus, hanya dinonaktifkan, karena penugasan, jadwal, dan nilai
-- lama tetap merujuk ke sini.
CREATE TABLE subjects (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    name       text        NOT NULL,
    code       text,
    is_active  boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT subjects_name_check CHECK (btrim(name) <> ''),
    -- Kode kosong disimpan sebagai NULL, bukan teks kosong.
    CONSTRAINT subjects_code_check CHECK (code IS NULL OR btrim(code) <> ''),
    -- Target foreign key komposit untuk penugasan dan jadwal (T65, T66), pola
    -- yang sama dengan fee_types dan classes.
    CONSTRAINT subjects_school_id_id_key UNIQUE (school_id, id)
);

-- Nama unik per sekolah tanpa membedakan huruf besar/kecil ("IPA" = "ipa").
CREATE UNIQUE INDEX subjects_school_id_name_key ON subjects (school_id, lower(name));

-- Kode, jika diisi, juga unik per sekolah. NULL tidak dianggap sama, jadi
-- banyak mapel tanpa kode diizinkan.
CREATE UNIQUE INDEX subjects_school_id_code_key ON subjects (school_id, lower(code));
