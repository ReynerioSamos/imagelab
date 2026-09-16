-- Filename: 000001_create_init_types.up.sql

BEGIN;

-- DATA-04 / JOB-02: constrains job status to exactly the four allowed
-- values at the database level, not just in application code. Any
-- INSERT/UPDATE using a value outside this list is rejected by
-- PostgreSQL itself.
CREATE TYPE job_status AS ENUM ('queued', 'processing', 'completed', 'failed');

-- Note on UUID generation: gen_random_uuid() has been built into
-- PostgreSQL core since version 13, so no CREATE EXTENSION is required
-- here. (Gatekeeper needed shim functions only because its migrations
-- called uuidv7()/uuidv4(), which are PG18+ names.)

COMMIT;