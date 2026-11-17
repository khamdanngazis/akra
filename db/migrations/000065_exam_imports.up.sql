-- Impor soal (T129–T131): Word, Moodle XML/GIFT, dan Excel memakai alur
-- yang sama: unggah → baca → pratinjau dan perbaiki → simpan sekaligus.
CREATE TABLE exam_imports (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid        NOT NULL REFERENCES schools (id),
    bank_id     uuid        NOT NULL,
    source      text        NOT NULL CHECK (source IN ('word', 'moodle', 'gift', 'excel')),
    file_name   text        NOT NULL,
    file_key    text,
    status      text        NOT NULL DEFAULT 'memproses' CHECK (status IN ('memproses', 'siap', 'gagal', 'selesai')),
    progress    int         NOT NULL DEFAULT 0,
    total       int         NOT NULL DEFAULT 0,
    error       text        NOT NULL DEFAULT '',
    -- Laporan akhir: jumlah soal siap, draf, dilewati.
    result      jsonb       NOT NULL DEFAULT '{}',
    created_by  uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CONSTRAINT exam_imports_school_id_id_key UNIQUE (school_id, id),
    CONSTRAINT exam_imports_bank_fkey FOREIGN KEY (school_id, bank_id) REFERENCES exam_banks (school_id, id),
    CONSTRAINT exam_imports_created_by_fkey FOREIGN KEY (school_id, created_by) REFERENCES users (school_id, id)
);
CREATE INDEX exam_imports_bank_idx ON exam_imports (school_id, bank_id, created_at DESC);

-- Hasil baca per butir. data berisi isian editor (QuestionForm) untuk soal
-- atau {title, body, numbered} untuk stimulus; bisa diperbaiki di pratinjau.
CREATE TABLE exam_import_items (
    import_id    uuid        NOT NULL,
    school_id    uuid        NOT NULL,
    seq          int         NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('soal', 'stimulus')),
    status       text        NOT NULL CHECK (status IN ('siap', 'cek', 'lewati')),
    problems     text[]      NOT NULL DEFAULT '{}',
    data         jsonb       NOT NULL,
    -- Soal: seq stimulus induknya di impor yang sama.
    stimulus_seq int,
    origin       text        NOT NULL DEFAULT '',
    number       text        NOT NULL DEFAULT '',
    PRIMARY KEY (import_id, seq),
    CONSTRAINT exam_import_items_import_fkey FOREIGN KEY (school_id, import_id) REFERENCES exam_imports (school_id, id) ON DELETE CASCADE
);
