-- Editor soal (T128): gambar soal, draf tersimpan otomatis, nomor baris
-- stimulus.

-- Gambar soal dan stimulus. File privat di storage; ditampilkan lewat URL
-- bertanda tangan yang kedaluwarsa (indeks MVP 4, standar 1).
CREATE TABLE exam_assets (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid        NOT NULL REFERENCES schools (id),
    bank_id      uuid        NOT NULL,
    storage_key  text        NOT NULL UNIQUE,
    content_type text        NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp', 'image/gif')),
    width        int         NOT NULL CHECK (width BETWEEN 1 AND 4000),
    height       int         NOT NULL CHECK (height BETWEEN 1 AND 4000),
    size_bytes   int         NOT NULL CHECK (size_bytes > 0),
    created_by   uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_assets_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_assets_bank_fkey FOREIGN KEY (school_id, bank_id) REFERENCES exam_banks (school_id, id),
    CONSTRAINT exam_assets_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);

-- Draf editor yang tersimpan otomatis tiap 5 detik. Satu draf per pengguna
-- per soal (atau per soal baru di satu bank). Menyimpan soal menghapus
-- drafnya. Draf tidak pernah mengubah soal yang sudah ada.
CREATE TABLE exam_question_drafts (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    bank_id     uuid        NOT NULL,
    question_id uuid,
    data        jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_question_drafts_user_fkey FOREIGN KEY (school_id, user_id) REFERENCES users (school_id, id),
    CONSTRAINT exam_question_drafts_bank_fkey FOREIGN KEY (school_id, bank_id) REFERENCES exam_banks (school_id, id),
    CONSTRAINT exam_question_drafts_question_fkey FOREIGN KEY (school_id, question_id) REFERENCES exam_questions (school_id, id),
    CONSTRAINT exam_question_drafts_size_check CHECK (octet_length(data::text) <= 524288)
);
CREATE UNIQUE INDEX exam_question_drafts_key ON exam_question_drafts
    (user_id, bank_id, coalesce(question_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- Stimulus bacaan dengan nomor baris (soal "pada baris ke-3").
ALTER TABLE exam_stimuli ADD COLUMN numbered boolean NOT NULL DEFAULT false;
