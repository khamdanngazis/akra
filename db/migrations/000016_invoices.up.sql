-- Target foreign key komposit: tagihan merujuk enrollment bersama sekolah
-- dan tahun ajarannya.
ALTER TABLE enrollments ADD CONSTRAINT enrollments_school_year_id_key UNIQUE (school_id, academic_year_id, id);

-- Tagihan siswa per periode. Terikat ke enrollment (bukan siswa) agar
-- tagihan kelas tetap benar saat siswa pindah kelas. Status "terlambat" tidak
-- disimpan; dihitung saat ditampilkan (status <> 'lunas' dan due_date lewat).
CREATE TABLE invoices (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    enrollment_id    uuid        NOT NULL,
    period           text        NOT NULL,
    due_date         date        NOT NULL,
    total_amount     bigint      NOT NULL DEFAULT 0,
    status           text        NOT NULL DEFAULT 'belum_bayar',
    voided_at        timestamptz,
    created_by       uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invoices_period_check CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    CONSTRAINT invoices_total_amount_check CHECK (total_amount >= 0),
    CONSTRAINT invoices_status_check CHECK (status IN ('belum_bayar', 'sebagian', 'lunas')),
    CONSTRAINT invoices_enrollment_fkey FOREIGN KEY (school_id, academic_year_id, enrollment_id)
        REFERENCES enrollments (school_id, academic_year_id, id),
    CONSTRAINT invoices_created_by_fkey FOREIGN KEY (school_id, created_by)
        REFERENCES users (school_id, id),
    CONSTRAINT invoices_school_id_id_key UNIQUE (school_id, id),
    -- Target foreign key item: item selalu ikut enrollment dan periode tagihannya.
    CONSTRAINT invoices_item_target_key UNIQUE (school_id, id, enrollment_id, period)
);

CREATE INDEX invoices_school_year_period_idx ON invoices (school_id, academic_year_id, period);
CREATE INDEX invoices_enrollment_id_idx ON invoices (enrollment_id);

-- Item tagihan. enrollment_id dan period disalin dari tagihan (dijaga foreign
-- key komposit) agar satu jenis tagihan per enrollment per periode bisa
-- ditegakkan dengan UNIQUE.
CREATE TABLE invoice_items (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid        NOT NULL REFERENCES schools (id),
    invoice_id    uuid        NOT NULL,
    enrollment_id uuid        NOT NULL,
    period        text        NOT NULL,
    fee_type_id   uuid        NOT NULL,
    description   text,
    amount        bigint      NOT NULL,
    -- Urutan tampil item dalam tagihan (sesuai urutan saat dibuat).
    position      smallint    NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invoice_items_amount_check CHECK (amount >= 0),
    CONSTRAINT invoice_items_invoice_fkey FOREIGN KEY (school_id, invoice_id, enrollment_id, period)
        REFERENCES invoices (school_id, id, enrollment_id, period),
    CONSTRAINT invoice_items_fee_type_fkey FOREIGN KEY (school_id, fee_type_id)
        REFERENCES fee_types (school_id, id),
    -- SPP bulan yang sama untuk enrollment yang sama tidak bisa tercatat dua kali.
    CONSTRAINT invoice_items_enrollment_fee_period_key UNIQUE (enrollment_id, fee_type_id, period)
);

CREATE INDEX invoice_items_invoice_id_idx ON invoice_items (invoice_id);
CREATE INDEX invoice_items_fee_type_id_idx ON invoice_items (fee_type_id);

-- total_amount tagihan harus sama dengan jumlah item. Diperiksa saat commit
-- (DEFERRABLE INITIALLY DEFERRED) agar tagihan dan item bisa disimpan dalam
-- urutan apa pun dalam satu transaksi.
CREATE FUNCTION invoice_total_matches_items() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    inv_id uuid;
    expected bigint;
    actual bigint;
BEGIN
    IF TG_TABLE_NAME = 'invoices' THEN
        inv_id := NEW.id;
    ELSIF TG_OP = 'DELETE' THEN
        inv_id := OLD.invoice_id;
    ELSE
        inv_id := NEW.invoice_id;
    END IF;
    SELECT total_amount INTO expected FROM invoices WHERE id = inv_id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    SELECT coalesce(sum(amount), 0) INTO actual FROM invoice_items WHERE invoice_id = inv_id;
    IF expected <> actual THEN
        RAISE EXCEPTION 'invoice % total_amount % does not match items sum %', inv_id, expected, actual
            USING ERRCODE = 'check_violation', CONSTRAINT = 'invoice_total_matches_items';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER invoice_items_total_check
    AFTER INSERT OR UPDATE OR DELETE ON invoice_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION invoice_total_matches_items();

CREATE CONSTRAINT TRIGGER invoices_total_check
    AFTER INSERT OR UPDATE OF total_amount ON invoices
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION invoice_total_matches_items();
