-- code identifies a school on the login form (and later, possibly a subdomain).
-- Lower-case letters, digits, and single hyphens; 3-32 characters.
ALTER TABLE schools
    ADD COLUMN code text NOT NULL,
    ADD CONSTRAINT schools_code_key UNIQUE (code),
    ADD CONSTRAINT schools_code_format CHECK (
        code ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(code) BETWEEN 3 AND 32
    );
