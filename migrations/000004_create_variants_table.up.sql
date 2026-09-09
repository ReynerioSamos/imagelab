-- Filename: 000004_create_variants_table.up.sql

BEGIN;

CREATE TABLE IF NOT EXISTS variants (
    id              bigserial   PRIMARY KEY,
    image_id        bigint      NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    name            text        NOT NULL, -- 'thumbnail' | 'preview' | 'display'
    stored_filename text        NOT NULL UNIQUE,
    width           integer     NOT NULL, -- actual output dimensions, not the target bounds
    height          integer     NOT NULL,
    size_bytes      bigint      NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (image_id, name) -- at most one row per variant name per image
);

COMMIT;
