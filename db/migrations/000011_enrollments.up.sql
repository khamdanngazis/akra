-- Target foreign key komposit: kelas dirujuk bersama sekolah dan tahun
-- ajarannya, sehingga database menjamin enrollment memakai kelas dari
-- sekolah dan tahun ajaran yang sama.
ALTER TABLE classes ADD CONSTRAINT classes_school_year_id_key UNIQUE (school_id, academic_year_id, id);

-- Riwayat penempatan siswa di kelas. Pindah kelas = tutup baris lama
-- (end_date) lalu buat baris baru; baris lama tidak pernah diubah kelasnya.
CREATE TABLE enrollments (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    student_id       uuid        NOT NULL,
    class_id         uuid        NOT NULL,
    academic_year_id uuid        NOT NULL,
    start_date       date        NOT NULL,
    -- NULL berarti masih aktif. end_date = start_date - 1 menandai penempatan
    -- yang dibatalkan di hari yang sama (periode kosong, riwayat tetap ada).
    end_date         date,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT enrollments_student_fkey FOREIGN KEY (school_id, student_id)
        REFERENCES students (school_id, id),
    CONSTRAINT enrollments_class_fkey FOREIGN KEY (school_id, academic_year_id, class_id)
        REFERENCES classes (school_id, academic_year_id, id),
    CONSTRAINT enrollments_dates_check CHECK (end_date IS NULL OR end_date >= start_date - 1),
    -- Target foreign key komposit untuk absensi dan tagihan.
    CONSTRAINT enrollments_school_id_id_key UNIQUE (school_id, id)
);

-- Satu enrollment aktif per siswa per tahun ajaran.
CREATE UNIQUE INDEX enrollments_one_active ON enrollments (student_id, academic_year_id) WHERE end_date IS NULL;
CREATE INDEX enrollments_class_id_idx ON enrollments (class_id);
CREATE INDEX enrollments_student_id_start_date_idx ON enrollments (student_id, start_date);
CREATE INDEX enrollments_school_id_academic_year_id_idx ON enrollments (school_id, academic_year_id);
