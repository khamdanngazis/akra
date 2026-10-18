-- Daftar tunggakan dan laporan memfilter tagihan per sekolah dan jatuh tempo.
CREATE INDEX invoices_school_due_date_idx ON invoices (school_id, due_date);

-- Jumlah pembayaran aktif mencari pembatalan yang disetujui per pembayaran.
CREATE INDEX payment_voids_payment_approved_idx ON payment_voids (payment_id) WHERE status = 'approved';
