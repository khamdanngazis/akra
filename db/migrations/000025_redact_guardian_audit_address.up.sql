-- Audit guardian.create lama menyimpan alamat lengkap wali (praktis alamat
-- rumah siswa). Ganti dengan tanda has_address, sama dengan format baru.
-- audit_logs append-only; trigger dimatikan sementara khusus untuk redaksi
-- data pribadi ini, di transaksi migrasi yang sama.
ALTER TABLE audit_logs DISABLE TRIGGER audit_logs_no_update_delete;

UPDATE audit_logs
SET after = (after - 'address') || jsonb_build_object('has_address', coalesce(after->>'address', '') <> '')
WHERE action = 'guardian.create' AND after ? 'address';

UPDATE audit_logs
SET before = (before - 'address') || jsonb_build_object('has_address', coalesce(before->>'address', '') <> '')
WHERE action LIKE 'guardian.%' AND before ? 'address';

ALTER TABLE audit_logs ENABLE TRIGGER audit_logs_no_update_delete;
