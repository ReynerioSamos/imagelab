package data

import "time"
//  Not used yet, mostly preparing the shape that jobs will eventually take once
// a proper worker exists
type Job struct {
	ID           int64      `json:"id"`
	ImageID      int64      `json:"image_id"`
	Status       string     `json:"status"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
}
