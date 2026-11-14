-- Tes seleksi pendaftaran dan nilainya (T93a). Hanya jalur dengan
-- uses_test yang dinilai. Skor akhir = jumlah (nilai / nilai_maks x bobot);
-- dihitung aplikasi, tidak disimpan.
CREATE TABLE admission_tests (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL REFERENCES schools (id),
    period_id    uuid        NOT NULL,
    name         text        NOT NULL,
    weight       smallint    NOT NULL,
    max_score    smallint    NOT NULL DEFAULT 100,
    scheduled_at timestamptz,
    location     text,
    sort_order   integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT admission_tests_period_fkey FOREIGN KEY (school_id, period_id) REFERENCES admission_periods (school_id, id),
    CONSTRAINT admission_tests_name_check CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 60),
    CONSTRAINT admission_tests_weight_check CHECK (weight BETWEEN 1 AND 100),
    CONSTRAINT admission_tests_max_check CHECK (max_score BETWEEN 1 AND 1000),
    CONSTRAINT admission_tests_location_check CHECK (char_length(location) <= 120),
    CONSTRAINT admission_tests_name_key UNIQUE (period_id, name),
    CONSTRAINT admission_tests_school_id_id_key UNIQUE (school_id, id)
);

CREATE TABLE applicant_test_scores (
    school_id    uuid        NOT NULL REFERENCES schools (id),
    applicant_id uuid        NOT NULL,
    test_id      uuid        NOT NULL,
    -- NULL = belum dinilai atau tidak hadir (bukan nol).
    score        numeric(6,2),
    recorded_by  uuid        NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (applicant_id, test_id),
    CONSTRAINT applicant_test_scores_applicant_fkey FOREIGN KEY (school_id, applicant_id) REFERENCES applicants (school_id, id),
    CONSTRAINT applicant_test_scores_test_fkey FOREIGN KEY (school_id, test_id) REFERENCES admission_tests (school_id, id),
    CONSTRAINT applicant_test_scores_recorded_by_fkey FOREIGN KEY (school_id, recorded_by) REFERENCES users (school_id, id),
    CONSTRAINT applicant_test_scores_score_check CHECK (score >= 0)
);
CREATE INDEX applicant_test_scores_test_idx ON applicant_test_scores (test_id);
