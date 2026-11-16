-- Tugas tambahan staf per tahun ajaran (T125). Bukan peran login: guru
-- dengan tugas koordinator_ujian mendapat hak koordinator modul ujian.
-- Tugas terikat tahun ajaran, sehingga tahun baru tidak mewarisi otomatis.
CREATE TABLE staff_duties (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    user_id          uuid        NOT NULL,
    duty             text        NOT NULL CHECK (duty IN ('koordinator_ujian')),
    created_by       uuid        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT staff_duties_unique UNIQUE (school_id, academic_year_id, user_id, duty),
    CONSTRAINT staff_duties_year_fkey FOREIGN KEY (school_id, academic_year_id)
        REFERENCES academic_years (school_id, id),
    CONSTRAINT staff_duties_user_fkey FOREIGN KEY (school_id, user_id)
        REFERENCES users (school_id, id),
    CONSTRAINT staff_duties_created_by_fkey FOREIGN KEY (school_id, created_by)
        REFERENCES users (school_id, id)
);

-- Cek "apakah pengguna ini koordinator tahun ini" di setiap request modul ujian.
CREATE INDEX staff_duties_user_idx ON staff_duties (school_id, user_id, academic_year_id);
