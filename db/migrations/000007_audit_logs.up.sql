-- Jejak audit append-only. Baris hanya boleh ditambah, tidak pernah diubah
-- atau dihapus.
CREATE TABLE audit_logs (
    id          bigserial   PRIMARY KEY,
    school_id   uuid        NOT NULL REFERENCES schools (id),
    -- NULL untuk aksi sistem. Tanpa foreign key agar jejak tetap utuh walau
    -- pengguna dihapus.
    user_id     uuid,
    action      text        NOT NULL CHECK (action <> ''),
    entity_type text        NOT NULL CHECK (entity_type <> ''),
    entity_id   text        NOT NULL CHECK (entity_id <> ''),
    before      jsonb,
    after       jsonb,
    ip          inet,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_school_id_created_at_idx ON audit_logs (school_id, created_at);
CREATE INDEX audit_logs_entity_idx ON audit_logs (entity_type, entity_id);

-- Trigger menolak UPDATE, DELETE, dan TRUNCATE untuk semua user, termasuk
-- pemilik tabel dan superuser. Ini perlindungan utama karena aplikasi dan
-- migrasi saat ini memakai user database yang sama (lihat README).
CREATE FUNCTION audit_logs_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER audit_logs_no_update_delete
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_append_only();

CREATE TRIGGER audit_logs_no_truncate
    BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION audit_logs_append_only();

-- Lapisan kedua untuk saat aplikasi memakai role terpisah dari pemilik tabel.
-- Tidak berpengaruh pada pemilik tabel atau superuser.
REVOKE UPDATE, DELETE, TRUNCATE ON audit_logs FROM PUBLIC;
