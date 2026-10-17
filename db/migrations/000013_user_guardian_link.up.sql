-- Akun portal orang tua: satu wali paling banyak satu akun, dan akun selalu
-- berada di sekolah yang sama dengan walinya.
ALTER TABLE users ADD COLUMN guardian_id uuid;
ALTER TABLE users ADD CONSTRAINT users_guardian_id_key UNIQUE (guardian_id);
ALTER TABLE users ADD CONSTRAINT users_guardian_fkey FOREIGN KEY (school_id, guardian_id)
    REFERENCES guardians (school_id, id);

-- Token aktivasi akun (sekali pakai). Yang disimpan hanya SHA-256 token.
CREATE TABLE activation_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT activation_tokens_hash_length CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX activation_tokens_user_id_idx ON activation_tokens (user_id);
