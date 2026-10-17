-- Pengaturan per sekolah (kunci-nilai), mis. void_approval_threshold.
CREATE TABLE school_settings (
    school_id  uuid        NOT NULL REFERENCES schools (id),
    key        text        NOT NULL,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (school_id, key),
    CONSTRAINT school_settings_key_check CHECK (key ~ '^[a-z][a-z0-9_]*$')
);

-- Pembatalan pembayaran. Baris payments tidak pernah diubah; pembayaran
-- dianggap batal jika punya pembatalan berstatus approved.
--
-- Alur status:
--   tanpa persetujuan (nominal <= ambang): langsung approved
--   perlu persetujuan: pending -> approved | rejected (oleh orang lain)
CREATE TABLE payment_voids (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid        NOT NULL REFERENCES schools (id),
    payment_id        uuid        NOT NULL,
    reason            text        NOT NULL,
    requested_by      uuid        NOT NULL,
    requires_approval boolean     NOT NULL,
    status            text        NOT NULL,
    decided_by        uuid,
    decided_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payment_voids_reason_check CHECK (btrim(reason) <> '' AND char_length(reason) <= 500),
    CONSTRAINT payment_voids_status_check CHECK (
        (status = 'pending' AND requires_approval AND decided_by IS NULL AND decided_at IS NULL)
        OR (status = 'approved' AND NOT requires_approval AND decided_by IS NULL AND decided_at IS NOT NULL)
        -- Pengajuan dan persetujuan/penolakan oleh orang yang berbeda.
        OR (status IN ('approved', 'rejected') AND requires_approval AND decided_by IS NOT NULL
            AND decided_by <> requested_by AND decided_at IS NOT NULL)
    ),
    CONSTRAINT payment_voids_payment_fkey FOREIGN KEY (school_id, payment_id)
        REFERENCES payments (school_id, id),
    CONSTRAINT payment_voids_requested_by_fkey FOREIGN KEY (school_id, requested_by)
        REFERENCES users (school_id, id),
    CONSTRAINT payment_voids_decided_by_fkey FOREIGN KEY (school_id, decided_by)
        REFERENCES users (school_id, id),
    CONSTRAINT payment_voids_school_id_id_key UNIQUE (school_id, id)
);

-- Satu pembayaran hanya bisa punya satu pembatalan yang menunggu atau
-- disetujui. Pembatalan yang ditolak tidak menghalangi pengajuan baru.
CREATE UNIQUE INDEX payment_voids_payment_active_key ON payment_voids (payment_id) WHERE status <> 'rejected';
CREATE INDEX payment_voids_school_status_idx ON payment_voids (school_id, status);

-- Baris pembatalan tidak boleh dihapus. Perubahan hanya keputusan atas
-- pengajuan pending (status, decided_by, decided_at); kolom lain tetap.
CREATE FUNCTION payment_voids_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'payment_voids is append-only: % is not allowed', TG_OP
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF OLD.status <> 'pending' OR NEW.status = 'pending'
        OR NEW.id <> OLD.id OR NEW.school_id <> OLD.school_id OR NEW.payment_id <> OLD.payment_id
        OR NEW.reason <> OLD.reason OR NEW.requested_by <> OLD.requested_by
        OR NEW.requires_approval <> OLD.requires_approval OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'payment_voids: only a pending void can be decided'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_voids_guard_update_delete
    BEFORE UPDATE OR DELETE ON payment_voids
    FOR EACH ROW EXECUTE FUNCTION payment_voids_guard();

CREATE TRIGGER payment_voids_no_truncate
    BEFORE TRUNCATE ON payment_voids
    FOR EACH STATEMENT EXECUTE FUNCTION payment_voids_guard();

REVOKE DELETE, TRUNCATE ON payment_voids FROM PUBLIC;
