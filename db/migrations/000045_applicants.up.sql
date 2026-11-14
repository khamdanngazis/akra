-- Pendaftar siswa baru (T90). Data rinci disimpan sebagai jsonb selama masih
-- pendaftar dan baru dipecah ke tabel siswa saat diterima (T96).
--
-- Nomor identitas (NIK/KITAS, No KK, no akta, KIP, KKS, dan NIK orang tua)
-- TIDAK BOLEH masuk jsonb: disimpan terenkripsi di kolom *_enc (T85b).
-- nik_idx adalah blind index untuk cek pendaftar ganda.
CREATE TABLE applicants (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid        NOT NULL REFERENCES schools (id),
    period_id           uuid        NOT NULL,
    track_id            uuid        NOT NULL,
    -- Diisi saat dikirim; draf belum punya nomor agar tidak ada nomor loncat.
    registration_number text,
    status              text        NOT NULL DEFAULT 'draf',
    full_name           text        NOT NULL,
    gender              char(1),
    birth_date          date        NOT NULL,
    -- Field non-sensitif students.ProfileFields, {key: nilai kanonik}.
    profile             jsonb       NOT NULL DEFAULT '{}',
    -- Orang tua non-sensitif, {"ayah": {...}, "ibu": {...}, "wali": {...}}.
    guardians           jsonb       NOT NULL DEFAULT '{}',
    nik_enc             bytea,
    nik_idx             bytea,
    kk_enc              bytea,
    birth_cert_enc      bytea,
    kip_enc             bytea,
    kks_enc             bytea,
    -- JSON terenkripsi {"ayah": "<NIK>", "ibu": "<NIK>", "wali": "<NIK>"}.
    guardian_niks_enc   bytea,
    -- Nomor HP orang tua untuk kontak dan melanjutkan draf (+62...).
    contact_phone       text        NOT NULL,
    consent_at          timestamptz NOT NULL,
    -- Petugas TU yang menginput (T92); NULL jika diisi sendiri secara online.
    entered_by          uuid,
    resume_code_hash    bytea,
    resume_failures     integer     NOT NULL DEFAULT 0,
    resume_locked_until timestamptz,
    submitted_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT applicants_period_fkey FOREIGN KEY (school_id, period_id) REFERENCES admission_periods (school_id, id),
    CONSTRAINT applicants_track_fkey FOREIGN KEY (school_id, track_id) REFERENCES admission_tracks (school_id, id),
    CONSTRAINT applicants_entered_by_fkey FOREIGN KEY (school_id, entered_by) REFERENCES users (school_id, id),
    CONSTRAINT applicants_status_check CHECK (status IN ('draf', 'dikirim', 'perlu_perbaikan', 'diverifikasi', 'diterima', 'cadangan', 'ditolak', 'mundur')),
    CONSTRAINT applicants_name_check CHECK (full_name = btrim(full_name) AND char_length(full_name) BETWEEN 1 AND 100),
    CONSTRAINT applicants_gender_check CHECK (gender IN ('L', 'P')),
    CONSTRAINT applicants_phone_check CHECK (contact_phone ~ '^\+[0-9]{8,15}$'),
    CONSTRAINT applicants_json_check CHECK (jsonb_typeof(profile) = 'object' AND jsonb_typeof(guardians) = 'object'),
    CONSTRAINT applicants_nik_pair_check CHECK ((nik_enc IS NULL) = (nik_idx IS NULL)),
    CONSTRAINT applicants_submitted_check CHECK ((status = 'draf') = (submitted_at IS NULL) AND (submitted_at IS NULL) = (registration_number IS NULL)),
    CONSTRAINT applicants_school_id_id_key UNIQUE (school_id, id)
);

CREATE UNIQUE INDEX applicants_school_number_key ON applicants (school_id, registration_number) WHERE registration_number IS NOT NULL;
-- Satu calon siswa (NIK) hanya sekali per periode, kecuali yang mundur atau
-- ditolak. Draf tidak dihitung agar orang tua bisa mengulang.
CREATE UNIQUE INDEX applicants_period_nik_key ON applicants (period_id, nik_idx)
    WHERE nik_idx IS NOT NULL AND status NOT IN ('draf', 'mundur', 'ditolak');
CREATE INDEX applicants_school_period_idx ON applicants (school_id, period_id, status);
CREATE INDEX applicants_school_phone_idx ON applicants (school_id, contact_phone) WHERE status IN ('draf', 'perlu_perbaikan');

-- Penghitung nomor pendaftaran per sekolah per tahun ajaran.
CREATE TABLE admission_counters (
    school_id        uuid    NOT NULL REFERENCES schools (id),
    academic_year_id uuid    NOT NULL,
    last_seq         integer NOT NULL DEFAULT 0,
    PRIMARY KEY (school_id, academic_year_id),
    CONSTRAINT admission_counters_year_fkey FOREIGN KEY (school_id, academic_year_id) REFERENCES academic_years (school_id, id)
);

-- Percobaan di halaman publik pendaftaran, untuk rate limit per IP dan per
-- kunci (hash nomor HP atau nomor pendaftaran; bukan nilai aslinya).
CREATE TABLE admission_attempts (
    id           bigserial   PRIMARY KEY,
    school_id    uuid        NOT NULL REFERENCES schools (id),
    kind         text        NOT NULL,
    key_hash     bytea,
    ip           inet,
    success      boolean     NOT NULL,
    attempted_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT admission_attempts_kind_check CHECK (kind IN ('draft', 'resume', 'status'))
);
CREATE INDEX admission_attempts_ip_idx ON admission_attempts (kind, ip, attempted_at);
CREATE INDEX admission_attempts_key_idx ON admission_attempts (kind, key_hash, attempted_at);
