-- Modul per sekolah (T163, ADR 0002). Satu baris per sekolah per modul
-- opsional; modul "core" selalu aktif dan tidak dicatat. Sekolah tanpa baris
-- untuk suatu modul memakai bawaan katalog (semua modul aktif kecuali ujian).
CREATE TABLE school_modules (
    school_id  uuid        NOT NULL REFERENCES schools (id),
    module     text        NOT NULL,
    enabled    boolean     NOT NULL,
    -- Pilihan per modul, mis. {"release": "r2"} untuk ujian.
    options    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    changed_at timestamptz NOT NULL DEFAULT now(),
    -- Siapa yang mengubah: tim implementator lewat CLI, mis. "akra-modul (budi)".
    changed_by text        NOT NULL DEFAULT '',
    note       text        NOT NULL DEFAULT '',
    PRIMARY KEY (school_id, module),
    CONSTRAINT school_modules_module_check CHECK (module IN
        ('attendance', 'schedule', 'grades', 'billing', 'admission', 'dapodik', 'exam', 'announcements', 'portal')),
    CONSTRAINT school_modules_options_check CHECK (jsonb_typeof(options) = 'object')
);

-- Label paket untuk tampilan dan laporan (mis. "lengkap"); konfigurasi
-- sebenarnya tetap daftar modul di school_modules.
ALTER TABLE schools ADD COLUMN package text NOT NULL DEFAULT '';

-- Sekolah yang sudah ada (keputusan 3): semua modul non-ujian aktif.
INSERT INTO school_modules (school_id, module, enabled, changed_by, note)
SELECT s.id, m, true, 'migrasi', 'sekolah lama: semua modul non-ujian aktif'
  FROM schools s
 CROSS JOIN unnest(ARRAY['attendance', 'schedule', 'grades', 'billing', 'admission', 'dapodik', 'announcements', 'portal']) AS m;

-- Ujian: dari kunci lama exam.enabled dan exam.release (T125).
INSERT INTO school_modules (school_id, module, enabled, options, changed_by, note)
SELECT s.id, 'exam',
       coalesce((SELECT ss.value = 'true' FROM school_settings ss WHERE ss.school_id = s.id AND ss.key = 'exam.enabled'), false),
       coalesce((SELECT jsonb_build_object('release', ss.value) FROM school_settings ss WHERE ss.school_id = s.id AND ss.key = 'exam.release'), '{}'::jsonb),
       'migrasi', 'dari pengaturan exam.enabled dan exam.release'
  FROM schools s;
