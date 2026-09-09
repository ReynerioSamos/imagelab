-- Filename: 000001_create_init_types.up.sql

BEGIN;

-- constrains job status to exactly the four allowed
-- values at the database level, not just in application code.
CREATE TYPE job_status AS ENUM ('queued', 'processing', 'completed', 'failed');

COMMIT;
