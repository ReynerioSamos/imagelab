-- Filename: 000002_create_images_table.up.sql

BEGIN;

CREATE TABLE IF NOT EXISTS images (
    id                bigserial   PRIMARY KEY,
    original_filename text        NOT NULL, -- display metadata only, never used as a filesystem path
    stored_filename   text        NOT NULL UNIQUE, -- server-generated, safe to use
    media_type        text        NOT NULL,
    size_bytes        bigint      NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

COMMIT;
