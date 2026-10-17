DROP TABLE IF EXISTS activation_tokens;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_guardian_fkey;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_guardian_id_key;
ALTER TABLE users DROP COLUMN IF EXISTS guardian_id;
