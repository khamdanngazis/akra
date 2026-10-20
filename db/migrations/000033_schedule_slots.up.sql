-- Jadwal pelajaran mingguan (T66). Guru setiap slot diambil dari penugasan
-- (T65) di service. Bentrok dicek di service (dengan kunci baris kelas dan
-- guru agar pesan jelas), dan dijaga lagi oleh constraint EXCLUDE di bawah
-- sehingga slot bentrok tidak mungkin tersimpan lewat jalur mana pun.
-- btree_gist dibutuhkan agar kolom uuid/smallint bisa dipakai di EXCLUDE
-- (ekstensi "trusted": cukup pemilik database).
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE schedule_slots (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    class_id         uuid        NOT NULL,
    subject_id       uuid        NOT NULL,
    teacher_id       uuid        NOT NULL,
    -- 1 = Senin sampai 6 = Sabtu.
    day_of_week      smallint    NOT NULL,
    start_time       time        NOT NULL,
    end_time         time        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT schedule_slots_day_check CHECK (day_of_week BETWEEN 1 AND 6),
    CONSTRAINT schedule_slots_time_check CHECK (end_time > start_time),
    CONSTRAINT schedule_slots_class_fkey FOREIGN KEY (school_id, academic_year_id, class_id)
        REFERENCES classes (school_id, academic_year_id, id),
    CONSTRAINT schedule_slots_subject_fkey FOREIGN KEY (school_id, subject_id)
        REFERENCES subjects (school_id, id),
    CONSTRAINT schedule_slots_teacher_fkey FOREIGN KEY (school_id, teacher_id)
        REFERENCES users (school_id, id),
    -- Rentang [mulai, selesai): slot yang bersebelahan (selesai 08.00, mulai
    -- 08.00) tidak dianggap bentrok.
    CONSTRAINT schedule_slots_class_overlap EXCLUDE USING gist (
        class_id WITH =, day_of_week WITH =,
        tsrange(date '2000-01-03' + start_time, date '2000-01-03' + end_time) WITH &&),
    CONSTRAINT schedule_slots_teacher_overlap EXCLUDE USING gist (
        teacher_id WITH =, day_of_week WITH =,
        tsrange(date '2000-01-03' + start_time, date '2000-01-03' + end_time) WITH &&)
);

CREATE INDEX schedule_slots_class_idx ON schedule_slots (school_id, class_id, day_of_week, start_time);
CREATE INDEX schedule_slots_teacher_idx ON schedule_slots (school_id, teacher_id, day_of_week, start_time);
