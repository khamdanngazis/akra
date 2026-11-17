-- Agenda ujian sekolah PTS/PAS (T134): ruang, jadwal mapel, pembagian
-- peserta ke ruang dan sesi, dan pengawas.

-- Ruang ujian per sekolah, dipakai ulang di agenda berikutnya.
CREATE TABLE exam_rooms (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL REFERENCES schools (id),
    name        text        NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 60),
    -- lab: komputer sekolah; kelas: peserta memakai HP.
    kind        text        NOT NULL CHECK (kind IN ('lab', 'kelas')),
    capacity    smallint    NOT NULL CHECK (capacity BETWEEN 1 AND 500),
    created_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT exam_rooms_school_id_id_key UNIQUE (school_id, id)
);
CREATE UNIQUE INDEX exam_rooms_name_key ON exam_rooms (school_id, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE exam_events (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid        NOT NULL REFERENCES schools (id),
    academic_year_id  uuid,
    name              text        NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 120),
    purpose           text        NOT NULL CHECK (purpose IN ('pts', 'pas')),
    starts_on         date        NOT NULL,
    ends_on           date        NOT NULL,
    grades            smallint[]  NOT NULL CHECK (cardinality(grades) BETWEEN 1 AND 12),
    -- Format nomor peserta, mis. {tahun}-{tingkat}-{urut:3}.
    number_format     text        NOT NULL DEFAULT '{tahun}-{tingkat}-{urut:3}' CHECK (length(number_format) BETWEEN 1 AND 60),
    -- Jeda antargelombang lab (menit): gelombang k mulai di jam slot +
    -- (k-1) × (lama slot + jeda).
    shift_gap_minutes smallint    NOT NULL DEFAULT 15 CHECK (shift_gap_minutes BETWEEN 0 AND 240),
    max_shifts        smallint    NOT NULL DEFAULT 3 CHECK (max_shifts BETWEEN 1 AND 6),
    announced_at      timestamptz,
    created_by        uuid        NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    archived_at       timestamptz,
    CONSTRAINT exam_events_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_events_year_fkey FOREIGN KEY (school_id, academic_year_id) REFERENCES academic_years (school_id, id),
    CONSTRAINT exam_events_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id),
    CONSTRAINT exam_events_dates_check CHECK (ends_on >= starts_on AND ends_on - starts_on <= 31)
);
CREATE INDEX exam_events_school_idx ON exam_events (school_id, starts_on DESC) WHERE archived_at IS NULL;

-- Ruang yang dipakai agenda.
CREATE TABLE exam_event_rooms (
    school_id uuid NOT NULL,
    event_id  uuid NOT NULL,
    room_id   uuid NOT NULL,
    PRIMARY KEY (event_id, room_id),
    CONSTRAINT exam_event_rooms_event_fkey FOREIGN KEY (school_id, event_id) REFERENCES exam_events (school_id, id),
    CONSTRAINT exam_event_rooms_room_fkey FOREIGN KEY (school_id, room_id) REFERENCES exam_rooms (school_id, id)
);

-- Baris jadwal: hari dan jam (waktu sekolah).
CREATE TABLE exam_event_slots (
    id         uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid     NOT NULL,
    event_id   uuid     NOT NULL,
    day        date     NOT NULL,
    start_time time     NOT NULL,
    end_time   time     NOT NULL,
    CONSTRAINT exam_event_slots_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_event_slots_event_fkey FOREIGN KEY (school_id, event_id) REFERENCES exam_events (school_id, id),
    CONSTRAINT exam_event_slots_time_check CHECK (end_time > start_time)
);
CREATE INDEX exam_event_slots_event_idx ON exam_event_slots (event_id, day, start_time);

-- Sel jadwal: satu mapel untuk satu tingkat di satu slot, menunjuk ujian.
CREATE TABLE exam_event_cells (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid NOT NULL,
    event_id    uuid NOT NULL,
    slot_id     uuid NOT NULL,
    grade_level smallint NOT NULL CHECK (grade_level BETWEEN 1 AND 12),
    subject_id  uuid NOT NULL,
    exam_id     uuid,
    CONSTRAINT exam_event_cells_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_event_cells_slot_grade_key UNIQUE (slot_id, grade_level),
    CONSTRAINT exam_event_cells_event_fkey FOREIGN KEY (school_id, event_id) REFERENCES exam_events (school_id, id),
    CONSTRAINT exam_event_cells_slot_fkey FOREIGN KEY (school_id, slot_id) REFERENCES exam_event_slots (school_id, id),
    CONSTRAINT exam_event_cells_subject_fkey FOREIGN KEY (school_id, subject_id) REFERENCES subjects (school_id, id),
    CONSTRAINT exam_event_cells_exam_fkey FOREIGN KEY (school_id, exam_id) REFERENCES exams (school_id, id)
);

-- Peserta agenda: ruang, gelombang (sesi lab), tempat duduk, nomor.
CREATE TABLE exam_event_participants (
    id             uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid     NOT NULL,
    event_id       uuid     NOT NULL,
    student_id     uuid     NOT NULL,
    class_id       uuid     NOT NULL,
    grade_level    smallint NOT NULL,
    room_id        uuid,
    shift          smallint CHECK (shift BETWEEN 1 AND 6),
    seat           smallint,
    participant_no text     NOT NULL DEFAULT '',
    CONSTRAINT exam_event_participants_student_key UNIQUE (event_id, student_id),
    CONSTRAINT exam_event_participants_event_fkey FOREIGN KEY (school_id, event_id) REFERENCES exam_events (school_id, id),
    CONSTRAINT exam_event_participants_student_fkey FOREIGN KEY (school_id, student_id) REFERENCES students (school_id, id),
    CONSTRAINT exam_event_participants_room_fkey FOREIGN KEY (school_id, room_id) REFERENCES exam_rooms (school_id, id),
    CONSTRAINT exam_event_participants_place_check CHECK ((room_id IS NULL) = (shift IS NULL))
);
CREATE INDEX exam_event_participants_room_idx ON exam_event_participants (event_id, room_id, shift);

-- Pengawas per ruang per slot per gelombang.
CREATE TABLE exam_event_proctors (
    school_id   uuid        NOT NULL,
    event_id    uuid        NOT NULL,
    room_id     uuid        NOT NULL,
    slot_id     uuid        NOT NULL,
    shift       smallint    NOT NULL CHECK (shift BETWEEN 1 AND 6),
    user_id     uuid        NOT NULL,
    assigned_by uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, room_id, slot_id, shift),
    CONSTRAINT exam_event_proctors_event_fkey FOREIGN KEY (school_id, event_id) REFERENCES exam_events (school_id, id),
    CONSTRAINT exam_event_proctors_room_fkey FOREIGN KEY (school_id, room_id) REFERENCES exam_rooms (school_id, id),
    CONSTRAINT exam_event_proctors_slot_fkey FOREIGN KEY (school_id, slot_id) REFERENCES exam_event_slots (school_id, id),
    CONSTRAINT exam_event_proctors_user_fkey FOREIGN KEY (school_id, user_id) REFERENCES users (school_id, id),
    CONSTRAINT exam_event_proctors_assigned_by_fkey FOREIGN KEY (school_id, assigned_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_event_proctors_user_idx ON exam_event_proctors (school_id, user_id);

-- Sesi PTS/PAS dibuat dari agenda: satu per sel × ruang × gelombang.
ALTER TABLE exam_sessions
    ADD COLUMN event_id uuid,
    ADD COLUMN room_id  uuid,
    ADD COLUMN cell_id  uuid,
    ADD COLUMN shift    smallint,
    ADD CONSTRAINT exam_sessions_event_fkey FOREIGN KEY (school_id, event_id) REFERENCES exam_events (school_id, id),
    ADD CONSTRAINT exam_sessions_room_fkey FOREIGN KEY (school_id, room_id) REFERENCES exam_rooms (school_id, id),
    ADD CONSTRAINT exam_sessions_cell_fkey FOREIGN KEY (school_id, cell_id) REFERENCES exam_event_cells (school_id, id);
CREATE INDEX exam_sessions_event_idx ON exam_sessions (event_id) WHERE event_id IS NOT NULL;
