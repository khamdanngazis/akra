DROP TABLE IF EXISTS invoice_items;
DROP TABLE IF EXISTS invoices;
DROP FUNCTION IF EXISTS invoice_total_matches_items();
ALTER TABLE enrollments DROP CONSTRAINT IF EXISTS enrollments_school_year_id_key;
