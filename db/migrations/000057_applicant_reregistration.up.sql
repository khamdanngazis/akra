-- Tanda daftar ulang pendaftar yang diterima (T120).
ALTER TABLE applicants ADD COLUMN reregistered_at timestamptz;
