-- Filename: 000003_create_jobs_table.up.sql
--
-- Schema is prepared in Week 1 (per the check-in requirement to "create
-- or prepare the image, job, and variant data model"), but nothing
-- writes to this table until Week 2's worker exists.

BEGIN;

CREATE TABLE IF NOT EXISTS jobs (
    -- Same reasoning as images.id: the job id is handed to the client in
    -- the 202 response and polled at GET /v1/jobs/{job_id}, so it must
    -- not be guessable or enumerable.
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    image_id      uuid        NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    status        job_status  NOT NULL DEFAULT 'queued',
    error_message text,                                  -- WRK-03: client-safe message only, set on failure
    queued_at     timestamptz NOT NULL DEFAULT now(),    -- DATA-01
    started_at    timestamptz,                           -- DATA-02: set when the worker claims the job
    completed_at  timestamptz,                           -- DATA-03: set only on success
    failed_at     timestamptz                            -- DATA-03: set only on processing failure
);

-- The worker's claim query filters on status and orders by queue time;
-- this keeps that lookup cheap once the table grows.
CREATE INDEX IF NOT EXISTS jobs_status_queued_at_idx ON jobs (status, queued_at);

COMMIT;