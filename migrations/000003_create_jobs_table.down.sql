-- Filename: 000003_create_jobs_table.down.sql

BEGIN;

DROP INDEX IF EXISTS jobs_status_queued_at_idx;
DROP TABLE IF EXISTS jobs;

COMMIT;