-- Satu sesi absensi per kelas per tanggal (tanggal lokal Asia/Jakarta).
CREATE TABLE attendance_sessions (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    class_id         uuid        NOT NULL,
    session_date     date        NOT NULL,
    created_by       uuid        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attendance_sessions_class_date_key UNIQUE (class_id, session_date),
    CONSTRAINT attendance_sessions_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT attendance_sessions_class_fkey FOREIGN KEY (school_id, academic_year_id, class_id)
        REFERENCES classes (school_id, academic_year_id, id),
    CONSTRAINT attendance_sessions_created_by_fkey FOREIGN KEY (school_id, created_by)
        REFERENCES users (school_id, id)
);

CREATE INDEX attendance_sessions_school_id_session_date_idx ON attendance_sessions (school_id, session_date);

-- Satu catatan per siswa (enrollment) per sesi. Catatan tidak dihapus;
-- koreksi dilakukan dengan mengubah status.
CREATE TABLE attendance_records (
    school_id     uuid        NOT NULL REFERENCES schools (id),
    session_id    uuid        NOT NULL,
    enrollment_id uuid        NOT NULL,
    status        text        NOT NULL DEFAULT 'hadir',
    note          text,
    updated_by    uuid,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, enrollment_id),
    CONSTRAINT attendance_records_status_check CHECK (status IN ('hadir', 'sakit', 'izin', 'alpa')),
    CONSTRAINT attendance_records_session_fkey FOREIGN KEY (school_id, session_id)
        REFERENCES attendance_sessions (school_id, id),
    CONSTRAINT attendance_records_enrollment_fkey FOREIGN KEY (school_id, enrollment_id)
        REFERENCES enrollments (school_id, id),
    CONSTRAINT attendance_records_updated_by_fkey FOREIGN KEY (school_id, updated_by)
        REFERENCES users (school_id, id)
);

CREATE INDEX attendance_records_enrollment_id_idx ON attendance_records (enrollment_id);
