-- Pengumuman sekolah untuk satu kelas atau semua kelas (class_id NULL) di
-- satu tahun ajaran. Tampil di portal orang tua yang anaknya terkait.
CREATE TABLE announcements (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    academic_year_id uuid        NOT NULL,
    class_id         uuid,
    title            text        NOT NULL,
    body             text        NOT NULL,
    published_at     timestamptz NOT NULL DEFAULT now(),
    created_by       uuid        NOT NULL,
    CONSTRAINT announcements_title_check CHECK (btrim(title) <> '' AND char_length(title) <= 150),
    CONSTRAINT announcements_body_check CHECK (btrim(body) <> '' AND char_length(body) <= 5000),
    CONSTRAINT announcements_year_fkey FOREIGN KEY (school_id, academic_year_id)
        REFERENCES academic_years (school_id, id),
    -- MATCH SIMPLE: class_id NULL (semua kelas) tidak diperiksa.
    CONSTRAINT announcements_class_fkey FOREIGN KEY (school_id, academic_year_id, class_id)
        REFERENCES classes (school_id, academic_year_id, id),
    CONSTRAINT announcements_created_by_fkey FOREIGN KEY (school_id, created_by)
        REFERENCES users (school_id, id)
);

CREATE INDEX announcements_school_year_published_idx ON announcements (school_id, academic_year_id, published_at DESC);
