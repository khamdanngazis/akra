-- Nomor kuitansi yang sudah terisi tetap ada (payments append-only).
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_receipt_number_check;
ALTER TABLE payments ALTER COLUMN receipt_number DROP NOT NULL;
DROP TABLE IF EXISTS receipt_counters;
