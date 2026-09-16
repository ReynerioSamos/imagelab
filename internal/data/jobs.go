package data

import "time"

// Job mirrors the jobs table (migrations/000003). Week 1 prepares this
// shape only; the claiming and state-transition methods (ClaimNext,
// MarkCompleted, MarkFailed) arrive in Week 2 alongside the worker that
// calls them.
//
// The nullable timestamps are pointers so that "not yet reached" is
// distinguishable from a zero time: StartedAt == nil means the job is
// still queued, CompletedAt == nil means it has not finished.
type Job struct {
	ID           string     `json:"id"`
	ImageID      string     `json:"image_id"`
	Status       string     `json:"status"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
}