-- Jenis tagihan (SPP, uang pangkal, kegiatan). Nominal selalu bilangan bulat
-- rupiah. Jenis tagihan tidak dihapus, hanya dinonaktifkan, karena tagihan
-- lama tetap merujuk ke sini.
CREATE TABLE fee_types (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    name            text        NOT NULL CHECK (btrim(name) <> ''),
    default_amount  bigint      NOT NULL,
    default_due_day smallint,
    is_active       boolean     NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    -- Nol diizinkan (jenis tagihan gratis), negatif tidak.
    CONSTRAINT fee_types_default_amount_check CHECK (default_amount >= 0),
    -- Batas 28 menghindari masalah bulan Februari.
    CONSTRAINT fee_types_default_due_day_check CHECK (default_due_day BETWEEN 1 AND 28),
    -- Target foreign key komposit untuk item tagihan (T30).
    CONSTRAINT fee_types_school_id_id_key UNIQUE (school_id, id)
);

-- Nama unik per sekolah tanpa membedakan huruf besar/kecil ("SPP" = "spp").
CREATE UNIQUE INDEX fee_types_school_id_name_key ON fee_types (school_id, lower(name));
