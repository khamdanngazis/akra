-- Nomor kuitansi berurutan per sekolah per tahun: KW/{tahun}/{6 digit}.
-- Nomor diambil dengan UPDATE ... RETURNING di transaksi yang sama dengan
-- insert pembayaran: baris counter terkunci sampai transaksi selesai,
-- sehingga dua pembayaran bersamaan tidak mendapat nomor yang sama, dan
-- rollback ikut mengembalikan nomornya (tidak ada celah).
CREATE TABLE receipt_counters (
    school_id   uuid    NOT NULL REFERENCES schools (id),
    year        integer NOT NULL,
    last_number bigint  NOT NULL DEFAULT 0,
    PRIMARY KEY (school_id, year),
    CONSTRAINT receipt_counters_year_check CHECK (year BETWEEN 2000 AND 9999),
    CONSTRAINT receipt_counters_last_number_check CHECK (last_number >= 0)
);

-- Beri nomor pembayaran yang sudah ada (urut waktu pencatatan, tahun dari
-- waktu pencatatan di Asia/Jakarta). payments append-only, jadi trigger
-- dimatikan sementara khusus untuk pengisian satu kali ini.
ALTER TABLE payments DISABLE TRIGGER payments_no_update_delete;

WITH numbered AS (
    SELECT id, school_id,
           extract(year FROM created_at AT TIME ZONE 'Asia/Jakarta')::integer AS year,
           row_number() OVER (PARTITION BY school_id, extract(year FROM created_at AT TIME ZONE 'Asia/Jakarta')
                              ORDER BY created_at, id) AS n
    FROM payments
    WHERE receipt_number IS NULL
)
UPDATE payments p
SET receipt_number = 'KW/' || numbered.year || '/' || lpad(numbered.n::text, 6, '0')
FROM numbered
WHERE p.id = numbered.id;

ALTER TABLE payments ENABLE TRIGGER payments_no_update_delete;

INSERT INTO receipt_counters (school_id, year, last_number)
SELECT school_id, split_part(receipt_number, '/', 2)::integer, max(split_part(receipt_number, '/', 3)::bigint)
FROM payments
GROUP BY school_id, split_part(receipt_number, '/', 2);

ALTER TABLE payments ALTER COLUMN receipt_number SET NOT NULL;
ALTER TABLE payments ADD CONSTRAINT payments_receipt_number_check CHECK (receipt_number ~ '^KW/[0-9]{4}/[0-9]{6,}$');
