-- Data rinci siswa sesuai Formulir Peserta Didik (F-PD) Dapodik (T86).
-- Satu baris per siswa; semua kolom data boleh NULL agar bisa dilengkapi
-- bertahap.
--
-- Nomor identitas (NIK/KITAS, No KK, no akta, KIP, KKS) disimpan
-- terenkripsi (kolom *_enc, AES-256-GCM di aplikasi, T85b). Kolom *_idx
-- adalah blind index (HMAC) untuk cek unik dan pencarian nilai lengkap.
-- Aturan format nomor-nomor itu dicek di service sebelum enkripsi, karena
-- database hanya melihat ciphertext.
--
-- Kode referensi Dapodik (agama, tempat tinggal, dst.) disimpan sebagai
-- teks dua digit; daftar lengkapnya di internal/dapodik/ref.
CREATE TABLE student_profiles (
    student_id        uuid        PRIMARY KEY,
    school_id         uuid        NOT NULL REFERENCES schools (id),

    -- Identitas
    nisn              text,
    nik_enc           bytea,
    nik_idx           bytea,
    kk_enc            bytea,
    kk_idx            bytea,
    birth_place       text,
    birth_cert_enc    bytea,
    religion          text,
    citizenship       text        NOT NULL DEFAULT 'WNI',
    country           text,
    special_needs     text[]      NOT NULL DEFAULT '{}',

    -- Alamat
    street            text,
    rt                text,
    rw                text,
    hamlet            text,
    village           text,
    district          text,
    postal_code       text,
    latitude          numeric(9,6),
    longitude         numeric(9,6),
    residence_type    text,
    transport_mode    text,

    -- Keluarga dan bantuan
    child_order       smallint,
    siblings          smallint,
    has_kip           boolean     NOT NULL DEFAULT false,
    kip_enc           bytea,
    kip_name          text,
    pip_eligible      boolean     NOT NULL DEFAULT false,
    pip_reason        text,
    kks_enc           bytea,

    -- Kontak
    home_phone        text,
    mobile_phone      text,
    email             text,

    -- Data periodik
    height_cm         smallint,
    weight_kg         smallint,
    head_cm           smallint,
    distance_km       numeric(6,2),
    travel_minutes    smallint,

    -- Registrasi
    registration_type text,
    entry_date        date,
    previous_school   text,
    exam_number       text,
    diploma_serial    text,
    skhun_number      text,

    updated_at        timestamptz NOT NULL DEFAULT now(),
    updated_by        uuid,

    CONSTRAINT student_profiles_student_fkey FOREIGN KEY (school_id, student_id) REFERENCES students (school_id, id),
    CONSTRAINT student_profiles_updated_by_fkey FOREIGN KEY (school_id, updated_by) REFERENCES users (school_id, id),

    CONSTRAINT student_profiles_nisn_check CHECK (nisn ~ '^[0-9]{10}$'),
    CONSTRAINT student_profiles_nik_pair_check CHECK ((nik_enc IS NULL) = (nik_idx IS NULL)),
    CONSTRAINT student_profiles_kk_pair_check CHECK ((kk_enc IS NULL) = (kk_idx IS NULL)),
    CONSTRAINT student_profiles_citizenship_check CHECK (citizenship IN ('WNI', 'WNA')),
    CONSTRAINT student_profiles_country_check CHECK (citizenship = 'WNA' OR country IS NULL),
    CONSTRAINT student_profiles_ref_check CHECK (
        religion ~ '^[0-9]{2}$' AND residence_type ~ '^[0-9]{2}$' AND transport_mode ~ '^[0-9]{2}$'
        AND pip_reason ~ '^[0-9]{2}$' AND registration_type ~ '^[0-9]{2}$'
    ),
    CONSTRAINT student_profiles_special_needs_check CHECK (array_to_string(special_needs, ',') ~ '^([0-9]{2}(,[0-9]{2})*)?$'),
    CONSTRAINT student_profiles_address_check CHECK (rt ~ '^[0-9]{1,3}$' AND rw ~ '^[0-9]{1,3}$' AND postal_code ~ '^[0-9]{5}$'),
    -- Wilayah Indonesia (dengan sedikit kelonggaran).
    CONSTRAINT student_profiles_latitude_check CHECK (latitude BETWEEN -11 AND 6),
    CONSTRAINT student_profiles_longitude_check CHECK (longitude BETWEEN 95 AND 141),
    CONSTRAINT student_profiles_numbers_check CHECK (
        child_order BETWEEN 1 AND 30 AND siblings BETWEEN 0 AND 30
        AND height_cm BETWEEN 30 AND 250 AND weight_kg BETWEEN 2 AND 250 AND head_cm BETWEEN 20 AND 80
        AND distance_km >= 0 AND travel_minutes BETWEEN 0 AND 1440
    ),
    CONSTRAINT student_profiles_email_check CHECK (email = lower(email) AND email LIKE '%_@_%'),
    CONSTRAINT student_profiles_kip_check CHECK (has_kip OR (kip_enc IS NULL AND kip_name IS NULL)),
    CONSTRAINT student_profiles_pip_check CHECK (pip_eligible OR pip_reason IS NULL)
);

-- NISN dan NIK unik per sekolah; sekolah lain boleh punya siswa yang sama
-- (pindahan).
CREATE UNIQUE INDEX student_profiles_school_nisn_key ON student_profiles (school_id, nisn) WHERE nisn IS NOT NULL;
CREATE UNIQUE INDEX student_profiles_school_nik_key ON student_profiles (school_id, nik_idx) WHERE nik_idx IS NOT NULL;
CREATE INDEX student_profiles_school_kk_idx ON student_profiles (school_id, kk_idx) WHERE kk_idx IS NOT NULL;
