-- Kredensial peserta ujian (T124): siswa atau calon siswa (pendaftar).
-- MVP 4 hanya membuat kode_ujian (kode di kartu peserta, per sesi);
-- password disiapkan untuk portal siswa berikutnya. Rahasia hanya
-- disimpan sebagai hash argon2id dan tidak pernah tercatat di log.
CREATE TABLE student_credentials (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    student_id      uuid,
    applicant_id    uuid,
    kind            text        NOT NULL CHECK (kind IN ('kode_ujian', 'password')),
    secret_hash     text        NOT NULL,
    -- Jendela mengerjakan: 30 menit sebelum sesi sampai sesi berakhir.
    valid_from      timestamptz,
    valid_until     timestamptz,
    -- Setelah valid_until, kode masih membuka hasil (hanya baca) sampai
    -- waktu ini; diisi saat hasil dibuka (T151). NULL = tidak ada.
    results_until   timestamptz,
    -- Sesi ujian (T134); foreign key ditambahkan saat tabel sesi dibuat.
    exam_session_id uuid,
    -- Perangkat pertama yang memakai kode; perangkat lain ditolak sampai
    -- pengawas mereset.
    device_id       uuid,
    failed_count    int         NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    locked_until    timestamptz,
    created_by      uuid        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    revoked_at      timestamptz,
    CONSTRAINT student_credentials_owner_check CHECK (num_nonnulls(student_id, applicant_id) = 1),
    CONSTRAINT student_credentials_window_check CHECK (
        kind <> 'kode_ujian' OR (valid_from IS NOT NULL AND valid_until IS NOT NULL AND valid_until > valid_from)),
    CONSTRAINT student_credentials_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT student_credentials_student_fkey FOREIGN KEY (school_id, student_id) REFERENCES students (school_id, id),
    CONSTRAINT student_credentials_applicant_fkey FOREIGN KEY (school_id, applicant_id) REFERENCES applicants (school_id, id),
    CONSTRAINT student_credentials_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);

-- Login: semua kredensial aktif satu peserta.
CREATE INDEX student_credentials_student_idx ON student_credentials (school_id, student_id) WHERE revoked_at IS NULL;
CREATE INDEX student_credentials_applicant_idx ON student_credentials (school_id, applicant_id) WHERE revoked_at IS NULL;

-- Rate limit dipakai bersama (T124): tabel percobaan pendaftaran publik
-- menjadi tabel umum. Jenis percobaan diatur oleh pemakainya.
ALTER TABLE admission_attempts RENAME TO rate_limit_attempts;
ALTER TABLE rate_limit_attempts DROP CONSTRAINT admission_attempts_kind_check;
ALTER TABLE rate_limit_attempts ADD CONSTRAINT rate_limit_attempts_kind_check CHECK (kind ~ '^[a-z][a-z_]*$');
ALTER INDEX admission_attempts_ip_idx RENAME TO rate_limit_attempts_ip_idx;
ALTER INDEX admission_attempts_key_idx RENAME TO rate_limit_attempts_key_idx;
