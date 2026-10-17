-- pgcrypto provides gen_random_uuid() on PostgreSQL < 13 and is kept for
-- consistency on newer versions.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
