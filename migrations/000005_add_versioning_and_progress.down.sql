-- -- Filename: 000005_add_versioning_and_progress.down.sql

-- Down Migration: Remove versioning, progress stages, and simulation from jobs
ALTER TABLE jobs 
DROP COLUMN IF EXISTS simulation_step,
DROP COLUMN IF EXISTS variants_completed,
DROP COLUMN IF EXISTS stage,
DROP COLUMN IF EXISTS version;