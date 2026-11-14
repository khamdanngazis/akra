-- Penanda siswa yang datanya sudah dimasukkan ke Dapodik lewat lembar salin
-- (T98).
ALTER TABLE student_profiles
    ADD COLUMN dapodik_entered_at timestamptz,
    ADD COLUMN dapodik_entered_by uuid,
    ADD CONSTRAINT student_profiles_dapodik_by_fkey FOREIGN KEY (school_id, dapodik_entered_by) REFERENCES users (school_id, id),
    ADD CONSTRAINT student_profiles_dapodik_pair_check CHECK ((dapodik_entered_at IS NULL) = (dapodik_entered_by IS NULL));
