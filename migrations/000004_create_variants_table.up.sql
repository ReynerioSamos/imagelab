-- Filename: 000004_create_variants_table.up.sql

BEGIN;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- for gen_random_uuid(); uuid-ossp is another option, but pgcrypto is more widely available.

CREATE TABLE IF NOT EXISTS variants (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    image_id        uuid        NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    name            text        NOT NULL, -- 'thumbnail' | 'preview' | 'display'
    stored_filename text        NOT NULL UNIQUE,
    width           integer     NOT NULL, -- IMG-04: actual output dimensions, not the target bounds
    height          integer     NOT NULL,
    size_bytes      bigint      NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (image_id, name) -- IMG-03: at most one row per variant name per image
);

COMMIT;