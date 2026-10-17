-- code mengidentifikasi sekolah di form login (dan nanti mungkin subdomain).
-- Huruf kecil, angka, dan tanda hubung tunggal; 3-32 karakter.
ALTER TABLE schools
    ADD COLUMN code text NOT NULL,
    ADD CONSTRAINT schools_code_key UNIQUE (code),
    ADD CONSTRAINT schools_code_format CHECK (
        code ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(code) BETWEEN 3 AND 32
    );
