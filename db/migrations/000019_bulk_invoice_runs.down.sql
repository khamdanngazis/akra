DROP INDEX IF EXISTS invoices_bulk_run_id_idx;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_bulk_run_fkey;
ALTER TABLE invoices DROP COLUMN IF EXISTS bulk_run_id;
DROP TABLE IF EXISTS bulk_invoice_runs;
