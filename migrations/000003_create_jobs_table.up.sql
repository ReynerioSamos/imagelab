-- Filename: 000003_create_jobs_table.up.sql
--
-- Schema is prepared but nothing
-- writes to this table until worker exists.

BEGIN;

CREATE TABLE IF NOT EXISTS jobs (
    id            bigserial   PRIMARY KEY,
    image_id      bigint      NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    status        job_status  NOT NULL DEFAULT 'queued',
    error_message text,                                 -- DATA-02/03: client-safe message only, set by MarkFailed
    queued_at     timestamptz NOT NULL DEFAULT now(),    -- DATA-01
    started_at    timestamptz,                           -- DATA-02: set when the worker claims the job
    completed_at  timestamptz,                           -- DATA-03: set only on success
    failed_at     timestamptz                            -- DATA-03: set only on processing failure -- distinct from completed_at
);

COMMIT;
