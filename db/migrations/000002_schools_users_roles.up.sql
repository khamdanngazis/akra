CREATE TABLE schools (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    npsn       text        UNIQUE,
    address    text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid        NOT NULL REFERENCES schools (id),
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    full_name     text        NOT NULL,
    is_active     boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- Email is unique per school, not globally.
    CONSTRAINT users_school_id_email_key UNIQUE (school_id, email),
    -- The application normalizes email to lower case (T06); this guards
    -- against writes that bypass it.
    CONSTRAINT users_email_lowercase CHECK (email = lower(email))
);

CREATE INDEX users_school_id_idx ON users (school_id);

CREATE TABLE roles (
    id   smallint PRIMARY KEY,
    code text     NOT NULL UNIQUE,
    name text     NOT NULL
);

CREATE TABLE user_roles (
    user_id uuid     NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id smallint NOT NULL REFERENCES roles (id),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX user_roles_role_id_idx ON user_roles (role_id);

INSERT INTO roles (id, code, name) VALUES
    (1, 'admin_tu',       'Admin/TU'),
    (2, 'bendahara',      'Bendahara'),
    (3, 'guru',           'Guru'),
    (4, 'kepala_sekolah', 'Kepala Sekolah'),
    (5, 'orang_tua',      'Orang Tua');
