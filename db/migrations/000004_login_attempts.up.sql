-- Satu baris per percobaan login, dipakai untuk lockout. school_code disimpan
-- seperti yang diketik (sudah dinormalisasi) karena sekolahnya bisa saja tidak ada.
CREATE TABLE login_attempts (
    id               bigserial   PRIMARY KEY,
    school_code      text        NOT NULL,
    email_normalized text        NOT NULL,
    ip               inet,
    success          boolean     NOT NULL,
    attempted_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX login_attempts_email_attempted_at_idx
    ON login_attempts (school_code, email_normalized, attempted_at);
CREATE INDEX login_attempts_ip_attempted_at_idx
    ON login_attempts (ip, attempted_at);
