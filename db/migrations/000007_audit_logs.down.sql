-- Menghapus seluruh jejak audit. Hanya untuk development.
DROP TABLE IF EXISTS audit_logs;
DROP FUNCTION IF EXISTS audit_logs_append_only();
