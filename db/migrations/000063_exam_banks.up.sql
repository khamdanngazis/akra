-- Bank soal (T127). Soal dipakai bersama per mapel; yang boleh mengubah
-- hanya pembuat dan admin_tu (keputusan 7 MVP 4). Tidak ada hapus permanen:
-- bank, stimulus, soal, dan TP diarsipkan.

CREATE TABLE exam_banks (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL REFERENCES schools (id),
    name        text        NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 120),
    -- NULL untuk bank tes seleksi umum (mis. Tes Potensi Akademik).
    subject_id  uuid,
    grade_level smallint    CHECK (grade_level BETWEEN 1 AND 12),
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT exam_banks_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_banks_subject_fkey FOREIGN KEY (school_id, subject_id) REFERENCES subjects (school_id, id),
    CONSTRAINT exam_banks_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_banks_school_idx ON exam_banks (school_id, subject_id) WHERE archived_at IS NULL;

-- Tujuan pembelajaran (TP) per mapel dan tingkat. Dipakai kisi-kisi,
-- analisis butir, dan deskripsi capaian rapor (T148, T150).
CREATE TABLE learning_objectives (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL REFERENCES schools (id),
    subject_id  uuid        NOT NULL,
    grade_level smallint    NOT NULL CHECK (grade_level BETWEEN 1 AND 12),
    code        text        NOT NULL CHECK (btrim(code) <> '' AND length(code) <= 30),
    description text        NOT NULL CHECK (btrim(description) <> '' AND length(description) <= 1000),
    sort_order  int         NOT NULL DEFAULT 0,
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT learning_objectives_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT learning_objectives_subject_fkey FOREIGN KEY (school_id, subject_id) REFERENCES subjects (school_id, id),
    CONSTRAINT learning_objectives_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
CREATE UNIQUE INDEX learning_objectives_code_key ON learning_objectives (school_id, subject_id, grade_level, lower(code))
    WHERE archived_at IS NULL;

-- Stimulus bersama: teks bacaan, tabel, atau gambar untuk beberapa soal.
CREATE TABLE exam_stimuli (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL REFERENCES schools (id),
    bank_id     uuid        NOT NULL,
    title       text        NOT NULL CHECK (btrim(title) <> '' AND length(title) <= 200),
    body_src    text        NOT NULL,
    body_html   text        NOT NULL,
    -- Teks polos yang dinormalkan (huruf kecil, tanpa harakat) untuk cari.
    search_text text        NOT NULL DEFAULT '',
    version     int         NOT NULL DEFAULT 1,
    created_by  uuid        NOT NULL,
    updated_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT exam_stimuli_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_stimuli_bank_fkey FOREIGN KEY (school_id, bank_id) REFERENCES exam_banks (school_id, id),
    CONSTRAINT exam_stimuli_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT exam_stimuli_updated_by_fkey FOREIGN KEY (school_id, updated_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_stimuli_bank_idx ON exam_stimuli (school_id, bank_id) WHERE archived_at IS NULL;

CREATE TABLE exam_questions (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    bank_id          uuid        NOT NULL,
    stimulus_id      uuid,
    stimulus_order   int,
    kind             text        NOT NULL CHECK (kind IN ('pg', 'pg_kompleks', 'benar_salah', 'isian', 'menjodohkan', 'esai')),
    body_src         text        NOT NULL,
    body_html        text        NOT NULL,
    -- Bagian yang boleh dilihat peserta (opsi, pernyataan, pasangan).
    options          jsonb       NOT NULL DEFAULT '{}',
    -- Kunci jawaban: kolom terpisah, tidak pernah ikut serialisasi peserta.
    answer_key       jsonb       NOT NULL DEFAULT '{}',
    points           smallint    NOT NULL DEFAULT 1 CHECK (points BETWEEN 1 AND 100),
    difficulty       text        CHECK (difficulty IN ('mudah', 'sedang', 'sulit')),
    cognitive_level  text        CHECK (cognitive_level IN ('C1', 'C2', 'C3', 'C4', 'C5', 'C6')),
    objective_id     uuid,
    -- Indikator soal untuk kisi-kisi (T132), opsional.
    indicator        text        NOT NULL DEFAULT '',
    tags             text[]      NOT NULL DEFAULT '{}',
    explanation_src  text        NOT NULL DEFAULT '',
    explanation_html text        NOT NULL DEFAULT '',
    status           text        NOT NULL DEFAULT 'draf' CHECK (status IN ('draf', 'ditelaah', 'siap')),
    search_text      text        NOT NULL DEFAULT '',
    version          int         NOT NULL DEFAULT 1,
    created_by       uuid        NOT NULL,
    updated_by       uuid        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    archived_at      timestamptz,
    CONSTRAINT exam_questions_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_questions_bank_fkey FOREIGN KEY (school_id, bank_id) REFERENCES exam_banks (school_id, id),
    CONSTRAINT exam_questions_stimulus_fkey FOREIGN KEY (school_id, stimulus_id) REFERENCES exam_stimuli (school_id, id),
    CONSTRAINT exam_questions_objective_fkey FOREIGN KEY (school_id, objective_id) REFERENCES learning_objectives (school_id, id),
    CONSTRAINT exam_questions_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT exam_questions_updated_by_fkey FOREIGN KEY (school_id, updated_by) REFERENCES users (school_id, id),
    CONSTRAINT exam_questions_stimulus_order_check CHECK ((stimulus_id IS NULL) = (stimulus_order IS NULL))
);
CREATE INDEX exam_questions_bank_idx ON exam_questions (school_id, bank_id, created_at) WHERE archived_at IS NULL;
CREATE INDEX exam_questions_stimulus_idx ON exam_questions (stimulus_id, stimulus_order) WHERE stimulus_id IS NOT NULL;

-- Snapshot setiap versi soal dan stimulus. Tidak pernah diubah: ujian
-- (T133) merujuk (question_id, version), sehingga menyunting soal tidak
-- mengubah ujian yang sudah memakainya.
CREATE TABLE exam_question_versions (
    school_id   uuid        NOT NULL,
    question_id uuid        NOT NULL,
    version     int         NOT NULL,
    snapshot    jsonb       NOT NULL,
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (question_id, version),
    CONSTRAINT exam_question_versions_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id)
);
CREATE TABLE exam_stimulus_versions (
    school_id   uuid        NOT NULL,
    stimulus_id uuid        NOT NULL,
    version     int         NOT NULL,
    snapshot    jsonb       NOT NULL,
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (stimulus_id, version),
    CONSTRAINT exam_stimulus_versions_stimulus_fkey FOREIGN KEY (school_id, stimulus_id) REFERENCES exam_stimuli (school_id, id)
);

-- Snapshot append-only: UPDATE dan DELETE ditolak.
CREATE FUNCTION exam_versions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'exam question/stimulus versions are append-only' USING ERRCODE = '42501';
END;
$$;
CREATE TRIGGER exam_question_versions_immutable BEFORE UPDATE OR DELETE ON exam_question_versions
    FOR EACH ROW EXECUTE FUNCTION exam_versions_immutable();
CREATE TRIGGER exam_stimulus_versions_immutable BEFORE UPDATE OR DELETE ON exam_stimulus_versions
    FOR EACH ROW EXECUTE FUNCTION exam_versions_immutable();

-- Statistik butir per pemakaian (diisi analisis butir T148).
CREATE TABLE exam_question_stats (
    id                 uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id          uuid          NOT NULL,
    question_id        uuid          NOT NULL,
    version            int           NOT NULL,
    -- Ujian sumber (T133); foreign key ditambahkan saat tabel ujian dibuat.
    exam_id            uuid,
    participants       int           NOT NULL CHECK (participants >= 0),
    difficulty_index   numeric(4,3)  CHECK (difficulty_index BETWEEN 0 AND 1),
    discrimination     numeric(4,3)  CHECK (discrimination BETWEEN -1 AND 1),
    computed_at        timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT exam_question_stats_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id)
);
CREATE INDEX exam_question_stats_question_idx ON exam_question_stats (question_id, computed_at DESC);
