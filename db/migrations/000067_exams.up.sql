-- Ujian (T133): ulangan guru, PTS/PAS, tes seleksi, dan ujian latihan.
-- Tidak ada hapus permanen; ujian diarsipkan. Soal ujian dibekukan saat
-- sesi pertama mulai (locked_at) lewat exam_item_snapshots.

CREATE TABLE exams (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid        NOT NULL REFERENCES schools (id),
    academic_year_id    uuid,
    name                text        NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 120),
    purpose             text        NOT NULL CHECK (purpose IN ('ulangan', 'pts', 'pas', 'seleksi', 'latihan')),
    -- NULL untuk tes seleksi umum.
    subject_id          uuid,
    grade_level         smallint    CHECK (grade_level BETWEEN 1 AND 12),
    duration_minutes    smallint    NOT NULL CHECK (duration_minutes BETWEEN 5 AND 600),
    -- Aturan.
    shuffle_questions   boolean     NOT NULL DEFAULT true,
    shuffle_options     boolean     NOT NULL DEFAULT true,
    -- Acak soal di dalam kelompok stimulus (bawaan: urutan tetap).
    shuffle_in_stimulus boolean     NOT NULL DEFAULT false,
    results_mode        text        NOT NULL DEFAULT 'nilai' CHECK (results_mode IN ('tidak', 'nilai', 'nilai_dan_pembahasan')),
    -- Hasil baru dibuka setelah sesi terakhir berakhir.
    results_after_all   boolean     NOT NULL DEFAULT true,
    max_attempts        smallint    NOT NULL DEFAULT 1 CHECK (max_attempts BETWEEN 1 AND 10),
    -- KKTP/nilai minimal 0–100; NULL = tidak ada.
    pass_score          smallint    CHECK (pass_score BETWEEN 0 AND 100),
    -- Integritas (T141): hanya catat, atau kunci setelah N kali keluar.
    integrity_mode      text        NOT NULL DEFAULT 'catat' CHECK (integrity_mode IN ('catat', 'kunci')),
    integrity_limit     smallint    CHECK (integrity_limit BETWEEN 1 AND 20),
    allow_back          boolean     NOT NULL DEFAULT true,
    variant_count       smallint    NOT NULL DEFAULT 1 CHECK (variant_count BETWEEN 1 AND 5),
    status              text        NOT NULL DEFAULT 'draf' CHECK (status IN ('draf', 'terjadwal', 'berlangsung', 'selesai', 'arsip')),
    -- Langkah wizard terakhir yang disimpan (untuk "lanjutkan").
    wizard_step         text        NOT NULL DEFAULT 'dasar',
    -- Pemilik: guru yang bertanggung jawab atas soal. Bisa berbeda dari
    -- pembuat bila koordinator membuatkan ujian PTS/PAS (T134).
    owner_id            uuid        NOT NULL,
    copied_from         uuid,
    scheduled_at        timestamptz,
    locked_at           timestamptz,
    created_by          uuid        NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    archived_at         timestamptz,
    CONSTRAINT exams_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exams_year_fkey FOREIGN KEY (school_id, academic_year_id) REFERENCES academic_years (school_id, id),
    CONSTRAINT exams_subject_fkey FOREIGN KEY (school_id, subject_id) REFERENCES subjects (school_id, id),
    CONSTRAINT exams_owner_fkey FOREIGN KEY (school_id, owner_id) REFERENCES users (school_id, id),
    CONSTRAINT exams_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT exams_copied_from_fkey FOREIGN KEY (school_id, copied_from) REFERENCES exams (school_id, id),
    CONSTRAINT exams_integrity_check CHECK ((integrity_mode = 'kunci') = (integrity_limit IS NOT NULL)),
    CONSTRAINT exams_subject_check CHECK (purpose = 'seleksi' OR subject_id IS NOT NULL)
);
CREATE INDEX exams_school_idx ON exams (school_id, created_at DESC) WHERE archived_at IS NULL;
CREATE INDEX exams_owner_idx ON exams (school_id, owner_id) WHERE archived_at IS NULL;

-- Bagian ujian (mis. TPA, Bahasa Inggris), masing-masing dengan urutan dan
-- instruksi sendiri. Setiap ujian punya minimal satu bagian.
CREATE TABLE exam_sections (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid        NOT NULL,
    exam_id           uuid        NOT NULL,
    position          int         NOT NULL,
    title             text        NOT NULL DEFAULT '' CHECK (length(title) <= 120),
    instructions_src  text        NOT NULL DEFAULT '',
    instructions_html text        NOT NULL DEFAULT '',
    CONSTRAINT exam_sections_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_sections_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id)
);
CREATE INDEX exam_sections_exam_idx ON exam_sections (exam_id, position);

-- Isi bagian: soal tetap ('soal') atau aturan ambil acak ('acak').
CREATE TABLE exam_items (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL,
    exam_id         uuid        NOT NULL,
    section_id      uuid        NOT NULL,
    position        int         NOT NULL,
    kind            text        NOT NULL CHECK (kind IN ('soal', 'acak')),
    question_id     uuid,
    -- Aturan acak.
    bank_id         uuid,
    rule_kind       text        CHECK (rule_kind IN ('pg', 'pg_kompleks', 'benar_salah', 'isian', 'menjodohkan', 'esai')),
    rule_difficulty text        CHECK (rule_difficulty IN ('mudah', 'sedang', 'sulit')),
    rule_objective  uuid,
    -- true: count adalah jumlah kelompok stimulus (beserta semua soalnya).
    rule_stimulus   boolean     NOT NULL DEFAULT false,
    rule_count      smallint    CHECK (rule_count BETWEEN 1 AND 200),
    CONSTRAINT exam_items_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_items_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_items_section_fkey FOREIGN KEY (school_id, section_id) REFERENCES exam_sections (school_id, id),
    CONSTRAINT exam_items_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id),
    CONSTRAINT exam_items_bank_fkey FOREIGN KEY (school_id, bank_id) REFERENCES exam_banks (school_id, id),
    CONSTRAINT exam_items_objective_fkey FOREIGN KEY (school_id, rule_objective) REFERENCES learning_objectives (school_id, id),
    CONSTRAINT exam_items_shape_check CHECK (
        (kind = 'soal' AND question_id IS NOT NULL AND bank_id IS NULL AND rule_count IS NULL)
        OR (kind = 'acak' AND question_id IS NULL AND bank_id IS NOT NULL AND rule_count IS NOT NULL))
);
CREATE INDEX exam_items_exam_idx ON exam_items (exam_id, section_id, position);
CREATE UNIQUE INDEX exam_items_question_key ON exam_items (exam_id, question_id) WHERE question_id IS NOT NULL;

-- Varian paket (A/B/C): set soal berbeda dengan komposisi sama. Isinya
-- dirakit ulang selama ujian belum terkunci.
CREATE TABLE exam_variants (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL,
    exam_id      uuid        NOT NULL,
    label        text        NOT NULL CHECK (label ~ '^[A-E]$'),
    seed         bigint      NOT NULL,
    assembled_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_variants_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_variants_label_key UNIQUE (exam_id, label),
    CONSTRAINT exam_variants_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id)
);

CREATE TABLE exam_variant_items (
    school_id   uuid NOT NULL,
    variant_id  uuid NOT NULL,
    position    int  NOT NULL,
    section_id  uuid NOT NULL,
    question_id uuid NOT NULL,
    stimulus_id uuid,
    PRIMARY KEY (variant_id, position),
    CONSTRAINT exam_variant_items_variant_fkey FOREIGN KEY (school_id, variant_id) REFERENCES exam_variants (school_id, id) ON DELETE CASCADE,
    CONSTRAINT exam_variant_items_section_fkey FOREIGN KEY (school_id, section_id) REFERENCES exam_sections (school_id, id),
    CONSTRAINT exam_variant_items_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id)
);

-- Soal ujian yang dibekukan saat ujian terkunci: merujuk versi soal dan
-- stimulus yang tidak pernah berubah, sehingga menyunting bank tidak
-- mengubah ujian. Tidak bisa diubah atau dihapus.
CREATE TABLE exam_item_snapshots (
    school_id        uuid     NOT NULL,
    exam_id          uuid     NOT NULL,
    variant_id       uuid     NOT NULL,
    position         int      NOT NULL,
    section_id       uuid     NOT NULL,
    question_id      uuid     NOT NULL,
    question_version int      NOT NULL,
    stimulus_id      uuid,
    stimulus_version int,
    points           smallint NOT NULL,
    PRIMARY KEY (variant_id, position),
    CONSTRAINT exam_item_snapshots_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_item_snapshots_variant_fkey FOREIGN KEY (school_id, variant_id) REFERENCES exam_variants (school_id, id),
    CONSTRAINT exam_item_snapshots_question_fkey FOREIGN KEY (question_id, question_version) REFERENCES exam_question_versions (question_id, version),
    CONSTRAINT exam_item_snapshots_stimulus_fkey FOREIGN KEY (stimulus_id, stimulus_version) REFERENCES exam_stimulus_versions (stimulus_id, version)
);
CREATE INDEX exam_item_snapshots_exam_idx ON exam_item_snapshots (exam_id);
CREATE TRIGGER exam_item_snapshots_immutable BEFORE UPDATE OR DELETE ON exam_item_snapshots
    FOR EACH ROW EXECUTE FUNCTION exam_versions_immutable();

-- Sesi ujian: satu waktu dan satu kelompok peserta. Ulangan guru: satu sesi
-- per kelas. PTS/PAS: per ruang per sesi (T134). Susulan/remedial: T137.
CREATE TABLE exam_sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL,
    exam_id      uuid        NOT NULL,
    kind         text        NOT NULL DEFAULT 'utama' CHECK (kind IN ('utama', 'susulan', 'remedial', 'pengayaan')),
    label        text        NOT NULL DEFAULT '' CHECK (length(label) <= 120),
    class_id     uuid,
    -- Tes seleksi: gelombang dan jalur sumber peserta (T149).
    period_id    uuid,
    track_id     uuid,
    location     text        NOT NULL DEFAULT 'di_ruang' CHECK (location IN ('di_ruang', 'jarak_jauh')),
    starts_at    timestamptz NOT NULL,
    ends_at      timestamptz NOT NULL,
    created_by   uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    cancelled_at timestamptz,
    CONSTRAINT exam_sessions_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_sessions_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id),
    CONSTRAINT exam_sessions_class_fkey FOREIGN KEY (class_id) REFERENCES classes (id),
    CONSTRAINT exam_sessions_period_fkey FOREIGN KEY (school_id, period_id) REFERENCES admission_periods (school_id, id),
    CONSTRAINT exam_sessions_track_fkey FOREIGN KEY (school_id, track_id) REFERENCES admission_tracks (school_id, id),
    CONSTRAINT exam_sessions_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT exam_sessions_window_check CHECK (ends_at > starts_at)
);
CREATE INDEX exam_sessions_exam_idx ON exam_sessions (exam_id, starts_at) WHERE cancelled_at IS NULL;
CREATE INDEX exam_sessions_school_time_idx ON exam_sessions (school_id, starts_at) WHERE cancelled_at IS NULL;

CREATE TABLE exam_session_participants (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid        NOT NULL,
    session_id     uuid        NOT NULL,
    student_id     uuid,
    applicant_id   uuid,
    -- Nomor peserta (T134); ulangan guru memakai NIS.
    participant_no text        NOT NULL DEFAULT '',
    variant_id     uuid,
    seat           smallint,
    -- Waktu tambahan untuk peserta berkebutuhan khusus (menit).
    extra_minutes  smallint    NOT NULL DEFAULT 0 CHECK (extra_minutes BETWEEN 0 AND 240),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_session_participants_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_session_participants_session_fkey FOREIGN KEY (school_id, session_id) REFERENCES exam_sessions (school_id, id),
    CONSTRAINT exam_session_participants_student_fkey FOREIGN KEY (school_id, student_id) REFERENCES students (school_id, id),
    CONSTRAINT exam_session_participants_applicant_fkey FOREIGN KEY (school_id, applicant_id) REFERENCES applicants (school_id, id),
    CONSTRAINT exam_session_participants_variant_fkey FOREIGN KEY (school_id, variant_id) REFERENCES exam_variants (school_id, id),
    CONSTRAINT exam_session_participants_owner_check CHECK (num_nonnulls(student_id, applicant_id) = 1)
);
CREATE UNIQUE INDEX exam_session_participants_student_key ON exam_session_participants (session_id, student_id) WHERE student_id IS NOT NULL;
CREATE UNIQUE INDEX exam_session_participants_applicant_key ON exam_session_participants (session_id, applicant_id) WHERE applicant_id IS NOT NULL;
CREATE INDEX exam_session_participants_student_idx ON exam_session_participants (school_id, student_id);

-- Pengawas sesi. Hak pengawas (T125) berlaku dari 30 menit sebelum sampai
-- 2 jam sesudah sesi.
CREATE TABLE exam_session_proctors (
    school_id   uuid        NOT NULL,
    session_id  uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    assigned_by uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id),
    CONSTRAINT exam_session_proctors_session_fkey FOREIGN KEY (school_id, session_id) REFERENCES exam_sessions (school_id, id),
    CONSTRAINT exam_session_proctors_user_fkey FOREIGN KEY (school_id, user_id) REFERENCES users (school_id, id),
    CONSTRAINT exam_session_proctors_assigned_by_fkey FOREIGN KEY (school_id, assigned_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_session_proctors_user_idx ON exam_session_proctors (school_id, user_id);

-- Kode ujian terikat ke sesi.
ALTER TABLE student_credentials ADD CONSTRAINT student_credentials_session_fkey
    FOREIGN KEY (school_id, exam_session_id) REFERENCES exam_sessions (school_id, id);
