-- Filename: 000005_add_versioning_and_progress.up.sql

-- Migration: Add versioning, progress stages, and simulation to jobs
ALTER TABLE jobs 
ADD COLUMN version INT NOT NULL DEFAULT 1,
ADD COLUMN stage VARCHAR(64) NOT NULL DEFAULT 'queued',
ADD COLUMN variants_completed INT NOT NULL DEFAULT 0,
ADD COLUMN simulation_step INT NOT NULL DEFAULT 0;

-- Backfill existing terminal jobs (if any) to have completed variant counts
UPDATE jobs 
SET variants_completed = 3, stage = 'display_ready' 
WHERE status = 'completed';