-- Periode (gelombang) dan jalur pendaftaran siswa baru (T89).
-- Pendaftaran terbuka jika status = 'buka' dan hari ini (WIB) di antara
-- opens_on dan closes_on (inklusif); aturan ini di service.
CREATE TABLE admission_periods (
    id                         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id                  uuid        NOT NULL REFERENCES schools (id),
    academic_year_id           uuid        NOT NULL,
    name                       text        NOT NULL,
    target_grade               smallint    NOT NULL,
    opens_on                   date        NOT NULL,
    closes_on                  date        NOT NULL,
    -- Biaya formulir dalam rupiah (bigint, bukan pecahan). 0 = gratis.
    fee_amount                 bigint      NOT NULL DEFAULT 0,
    fee_required_before_verify boolean     NOT NULL DEFAULT false,
    required_docs              text[]      NOT NULL DEFAULT '{}',
    status                     text        NOT NULL DEFAULT 'draf',
    created_by                 uuid        NOT NULL,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT admission_periods_year_fkey FOREIGN KEY (school_id, academic_year_id) REFERENCES academic_years (school_id, id),
    CONSTRAINT admission_periods_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT admission_periods_name_check CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT admission_periods_grade_check CHECK (target_grade BETWEEN 1 AND 12),
    CONSTRAINT admission_periods_dates_check CHECK (closes_on >= opens_on),
    CONSTRAINT admission_periods_fee_check CHECK (fee_amount >= 0),
    CONSTRAINT admission_periods_docs_check CHECK (required_docs <@ ARRAY['kk', 'akta', 'ijazah', 'foto', 'kip']),
    CONSTRAINT admission_periods_status_check CHECK (status IN ('draf', 'buka', 'tutup')),
    CONSTRAINT admission_periods_name_key UNIQUE (school_id, academic_year_id, name),
    CONSTRAINT admission_periods_school_id_id_key UNIQUE (school_id, id)
);

CREATE TABLE admission_tracks (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    period_id  uuid        NOT NULL,
    name       text        NOT NULL,
    -- NULL = tanpa batas kuota.
    quota      integer,
    -- Jalur yang memakai tes seleksi (T93a).
    uses_test  boolean     NOT NULL DEFAULT false,
    sort_order integer     NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT admission_tracks_period_fkey FOREIGN KEY (school_id, period_id) REFERENCES admission_periods (school_id, id),
    CONSTRAINT admission_tracks_name_check CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 60),
    CONSTRAINT admission_tracks_quota_check CHECK (quota > 0),
    CONSTRAINT admission_tracks_name_key UNIQUE (period_id, name),
    CONSTRAINT admission_tracks_school_id_id_key UNIQUE (school_id, id)
);

CREATE INDEX admission_periods_school_idx ON admission_periods (school_id, opens_on DESC);
CREATE INDEX admission_tracks_period_idx ON admission_tracks (period_id, sort_order);
