-- pgcrypto menyediakan gen_random_uuid() di PostgreSQL < 13 dan tetap
-- dipasang di versi baru agar konsisten.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
