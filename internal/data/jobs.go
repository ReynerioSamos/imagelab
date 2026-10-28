package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Job mirrors the jobs table (migrations/000003). Week 1 prepares this
// shape only; the claiming and state-transition methods (ClaimNext,
// MarkCompleted, MarkFailed) arrive in Week 2 alongside the worker that
// calls them.
//
// The nullable timestamps are pointers so that "not yet reached" is
// distinguishable from a zero time: StartedAt == nil means the job is
// still queued, CompletedAt == nil means it has not finished.
type Job struct {
	ID           			string     `json:"id"`
	ImageID      			string     `json:"image_id"`
	Status       			string     `json:"status"`
	Stage					string     `json:"stage"`
	Version      			int        `json:"version"`
	VariantsCompleted 		int        `json:"variants_completed"`
	SimulationStep 			int        `json:"simulation_step"`
	ErrorMessage 			*string    `json:"error_message,omitempty"`
	QueuedAt     			time.Time  `json:"queued_at"`
	StartedAt    			*time.Time `json:"started_at,omitempty"`
	CompletedAt  			*time.Time `json:"completed_at,omitempty"`
	FailedAt     			*time.Time `json:"failed_at,omitempty"`
}

type JobModel struct {
	DB *sql.DB
}

func (m JobModel) Insert(imageID string) (*Job, error) {
	// 1. Generate UUID v7
	jobID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	job := &Job{
		ID:      jobID.String(), // Fixes: declared and not used: newID
		ImageID: imageID,
		Status:  "queued",
	}

	query := `
		INSERT INTO jobs (id, image_id, status)
		VALUES ($1, $2, $3)
		RETURNING queued_at`

	// 2. Use '=' instead of ':=' for err because 'err' was already declared on line 27
	// Fixes: no new variables on left side of :=
	err = m.DB.QueryRow(query, job.ID, job.ImageID, job.Status).Scan(&job.QueuedAt)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (m JobModel) Get(id string) (*Job, error) {
	query := `
		SELECT id, image_id, status, stage, version, variants_completed, simulation_step, error_message, queued_at, started_at, completed_at, failed_at
		FROM jobs
		WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var job Job
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&job.ID,
		&job.ImageID,
		&job.Status,
		&job.Stage,
		&job.Version,
		&job.VariantsCompleted,
		&job.SimulationStep,
		&job.ErrorMessage,
		&job.QueuedAt,
		&job.StartedAt,
		&job.CompletedAt,
		&job.FailedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	return &job, nil
}

// ClaimNext claims the oldest queued job atomically for worker execution.
// FOR UPDATE SKIP LOCKED prevents concurrent workers from locking the same job row.
func (m JobModel) ClaimNext() (*Job, error) {
	query := `
		UPDATE jobs
		SET status = 'processing', started_at = NOW()
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'queued'
			ORDER BY queued_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, image_id, status, stage, version, variants_completed, simulation_step, queued_at, started_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var job Job
	err := m.DB.QueryRowContext(ctx, query).Scan(
		&job.ID,
		&job.ImageID,
		&job.Status,
		&job.Stage,
		&job.Version,
		&job.VariantsCompleted,
		&job.SimulationStep,
		&job.QueuedAt,
		&job.StartedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No jobs queued
		}
		return nil, err
	}

	return &job, nil
}

func (m JobModel) MarkCompleted(jobID string) error {
	query := `
		UPDATE jobs
		SET status = 'completed', completed_at = NOW()
		WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, jobID)
	return err
}

func (m JobModel) MarkFailed(jobID string, errMsg string) error {
	query := `
		UPDATE jobs
		SET status = 'failed', error_message = $1, failed_at = NOW()
		WHERE id = $2`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, errMsg, jobID)
	return err
}

// Test 2 Implementation of UpdateProgressTx, MarkCompletedTx, and MarkFailedTx methods for long polling implementation to work alongside the old Test 1 - Short polling
// These are transactional methods to ensure that the job state is updated consistently and that any observers waiting for changes are notified after the transaction commits.


// UpdateProgressTx updates the job stage, increments variants_completed, and bumps version atomically within a transaction.
func (m JobModel) UpdateProgressTx(ctx context.Context, tx *sql.Tx, jobID string, stage string, variantsCompleted int) (int, error) {
	query := `
		UPDATE jobs
		SET stage = $1, 
		    variants_completed = $2, 
		    version = version + 1
		WHERE id = $3
		RETURNING version`

	var newVersion int
	err := tx.QueryRowContext(ctx, query, stage, variantsCompleted, jobID).Scan(&newVersion)
	return newVersion, err
}

// MarkCompletedTx transitions the job to completed, updates stage/variants, and bumps version atomically within a transaction.
func (m JobModel) MarkCompletedTx(ctx context.Context, tx *sql.Tx, jobID string, variantsCompleted int) (int, error) {
	query := `
		UPDATE jobs
		SET status = 'completed', 
		    stage = 'completed', 
		    variants_completed = $1, 
		    version = version + 1, 
		    completed_at = NOW()
		WHERE id = $2
		RETURNING version`

	var newVersion int
	err := tx.QueryRowContext(ctx, query, variantsCompleted, jobID).Scan(&newVersion)
	return newVersion, err
}

// MarkFailedTx transitions the job to failed, records error message, and bumps version atomically within a transaction.
func (m JobModel) MarkFailedTx(ctx context.Context, tx *sql.Tx, jobID string, errMsg string) (int, error) {
	query := `
		UPDATE jobs
		SET status = 'failed', 
		    stage = 'failed', 
		    error_message = $1, 
		    version = version + 1, 
		    failed_at = NOW()
		WHERE id = $2
		RETURNING version`

	var newVersion int
	err := tx.QueryRowContext(ctx, query, errMsg, jobID).Scan(&newVersion)
	return newVersion, err
}