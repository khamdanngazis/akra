-- Riwayat perubahan status pendaftar (T93). actor_id NULL berarti
-- dilakukan pendaftar sendiri (kirim/kirim ulang).
CREATE TABLE applicant_events (
    id           bigserial   PRIMARY KEY,
    school_id    uuid        NOT NULL REFERENCES schools (id),
    applicant_id uuid        NOT NULL,
    from_status  text        NOT NULL,
    to_status    text        NOT NULL,
    note         text,
    actor_id     uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT applicant_events_applicant_fkey FOREIGN KEY (school_id, applicant_id) REFERENCES applicants (school_id, id),
    CONSTRAINT applicant_events_actor_fkey FOREIGN KEY (school_id, actor_id) REFERENCES users (school_id, id),
    CONSTRAINT applicant_events_note_check CHECK (char_length(note) <= 1000)
);
CREATE INDEX applicant_events_applicant_idx ON applicant_events (applicant_id, created_at);


-- Siswa hasil penerimaan (T96). Pendaftar yang sudah menjadi siswa tidak
-- bisa diubah statusnya lagi.
ALTER TABLE applicants ADD COLUMN student_id uuid,
    ADD CONSTRAINT applicants_student_fkey FOREIGN KEY (school_id, student_id) REFERENCES students (school_id, id);
CREATE UNIQUE INDEX applicants_student_key ON applicants (student_id) WHERE student_id IS NOT NULL;
