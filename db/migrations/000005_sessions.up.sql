-- Skema yang dibutuhkan github.com/alexedwards/scs/pgxstore.
-- data berisi nilai sesi hasil encode gob; jangan simpan password di sini.
CREATE TABLE sessions (
    token  text        PRIMARY KEY,
    data   bytea       NOT NULL,
    expiry timestamptz NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);
