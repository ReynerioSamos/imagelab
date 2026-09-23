-- Filename: 000002_create_images_table.up.sql

BEGIN;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- for gen_random_uuid(); uuid-ossp is another option, but pgcrypto is more widely available.

CREATE TABLE IF NOT EXISTS images (
    -- UUID rather than bigserial: the image id appears in client-facing
    -- URLs (GET /v1/images/{image_id}/variants/{name}), and a sequential
    -- integer would leak how many images the system has stored and let
    -- anyone enumerate other users' images by decrementing the number.
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    original_filename text        NOT NULL, -- VAL-04: display metadata only, never used as a filesystem path
    stored_filename   text        NOT NULL UNIQUE, -- VAL-03: server-generated, safe to use as a path segment
    media_type        text        NOT NULL,
    size_bytes        bigint      NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

COMMIT;