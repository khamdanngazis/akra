-- Data rinci orang tua/wali sesuai F-PD Dapodik (T87). NIK disimpan
-- terenkripsi di aplikasi (T85b); nik_idx adalah blind index untuk cek unik
-- dan pencocokan wali kakak-adik.
ALTER TABLE guardians
    ADD COLUMN nik_enc       bytea,
    ADD COLUMN nik_idx       bytea,
    ADD COLUMN birth_year    smallint,
    ADD COLUMN education     text,
    ADD COLUMN occupation    text,
    ADD COLUMN income        text,
    ADD COLUMN special_needs text[] NOT NULL DEFAULT '{}',
    ADD CONSTRAINT guardians_nik_pair_check CHECK ((nik_enc IS NULL) = (nik_idx IS NULL)),
    ADD CONSTRAINT guardians_birth_year_check CHECK (birth_year BETWEEN 1900 AND 2100),
    ADD CONSTRAINT guardians_ref_check CHECK (education ~ '^[0-9]{2}$' AND occupation ~ '^[0-9]{2}$' AND income ~ '^[0-9]{2}$'),
    ADD CONSTRAINT guardians_special_needs_check CHECK (array_to_string(special_needs, ',') ~ '^([0-9]{2}(,[0-9]{2})*)?$');

-- Satu orang tua dengan NIK yang sama cukup satu data wali per sekolah.
CREATE UNIQUE INDEX guardians_school_nik_key ON guardians (school_id, nik_idx) WHERE nik_idx IS NOT NULL;

-- Penanda ayah/ibu kandung. Hanya bermakna untuk hubungan ayah dan ibu.
ALTER TABLE student_guardians ADD COLUMN is_biological boolean NOT NULL DEFAULT true;

-- Data lama bisa punya dua "ayah" atau dua "ibu" untuk satu siswa. Yang
-- paling awal dianggap kandung; sisanya ditandai bukan kandung agar indeks
-- di bawah bisa dibuat. TU bisa memperbaikinya di form data rinci.
UPDATE student_guardians sg SET is_biological = false
FROM (
    SELECT student_id, guardian_id,
           row_number() OVER (PARTITION BY student_id, relationship ORDER BY created_at, guardian_id) AS rn
    FROM student_guardians WHERE relationship IN ('ayah', 'ibu')
) d
WHERE sg.student_id = d.student_id AND sg.guardian_id = d.guardian_id AND d.rn > 1;

CREATE UNIQUE INDEX student_one_biological_father ON student_guardians (student_id) WHERE relationship = 'ayah' AND is_biological;
CREATE UNIQUE INDEX student_one_biological_mother ON student_guardians (student_id) WHERE relationship = 'ibu' AND is_biological;
