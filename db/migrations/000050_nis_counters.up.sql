-- Penghitung NIS otomatis saat pendaftar diterima menjadi siswa (T96).
-- prefix adalah format NIS sekolah tanpa bagian nomor urut, mis. "2027"
-- untuk format {yyyy}{seq:3}; setiap awalan punya urutan sendiri.
CREATE TABLE nis_counters (
    school_id uuid    NOT NULL REFERENCES schools (id),
    prefix    text    NOT NULL,
    last_seq  integer NOT NULL DEFAULT 0,
    PRIMARY KEY (school_id, prefix),
    CONSTRAINT nis_counters_seq_check CHECK (last_seq >= 0)
);
