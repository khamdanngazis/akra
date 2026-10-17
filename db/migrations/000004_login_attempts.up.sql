-- One row per login attempt, used for lockout. school_code is stored as typed
-- (normalized) because the school may not exist.
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
