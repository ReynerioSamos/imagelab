package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"imagelab/internal/data"
)

// sseJobHandler implements the Server-Sent Events endpoint for job observation.
func (app *application) sseJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if jobID == "" {
		app.notFoundResponse(w, r)
		return
	}

	// SSE-02: Return 404 for an unknown job BEFORE beginning a stream.
	job, err := app.models.Jobs.Get(jobID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// SSE-03: Set required SSE headers.
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Note: If deploying behind a reverse proxy like Nginx, you may also need 
	// "X-Accel-Buffering: no" to prevent the proxy from buffering the stream.

	// SSE-06: Get ResponseController to enable flushing.
	rc := http.NewResponseController(w)

	// SYNC-03 & SYNC-06: Subscribe to the notification hub. 
	// Defer ensures cleanup on every exit path (disconnect, terminal state, or error).
	ch := globalHub.Subscribe(jobID)
	defer globalHub.Unsubscribe(jobID, ch)

	// Helper function to format, write, and flush an SSE event frame.
	writeSSEEvent := func(eventName string, payload any) error {
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		// Required frame shape: event: <name>\ndata: <json>\n\n
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes)); err != nil {
			return err
		}
		// Flush makes the frame available to the browser immediately.
		return rc.Flush()
	}

	// SSE-04: Send the current complete job snapshot immediately upon connection.
	initialPayload := app.buildSSEJobPayload(job)
	if err := writeSSEEvent("job-update", initialPayload); err != nil {
		app.logger.Error("failed to write initial SSE event", "job_id", jobID, "error", err)
		return
	}

	lastVersion := job.Version

	// SSE-05 & SSE-07: Event loop for later updates.
	for {
		select {
		// SYNC-07: Request cancellation ends the observer handler only.
		case <-r.Context().Done():
			app.logger.Info("SSE client disconnected", "job_id", jobID)
			return

		case <-ch:
			// SYNC-04: Treat notification only as a signal to re-read PostgreSQL.
			currentJob, err := app.models.Jobs.Get(jobID)
			if err != nil {
				app.logger.Error("failed to fetch job on SSE notification", "job_id", jobID, "error", err)
				continue
			}

			// SYNC-05: Send only if version is newer (handles coalesced notifications).
			if currentJob.Version > lastVersion {
				lastVersion = currentJob.Version
				payload := app.buildSSEJobPayload(currentJob)

				if err := writeSSEEvent("job-update", payload); err != nil {
					app.logger.Error("failed to write SSE update", "job_id", jobID, "error", err)
					return
				}

				// SSE-07: End the server stream after sending a terminal snapshot.
				if currentJob.Status == "completed" || currentJob.Status == "failed" {
					app.logger.Info("SSE stream ending due to terminal state", "job_id", jobID, "status", currentJob.Status)
					return
				}
			}
		}
	}
}

// buildSSEJobPayload formats the job representation identically to the long-poll response.
// This ensures the frontend can use the exact same rendering function for both modes.
func (app *application) buildSSEJobPayload(job *data.Job) map[string]any {
	res := map[string]any{
		"id":                 job.ID,
		"image_id":           job.ImageID,
		"status":             job.Status,
		"version":            job.Version,
		"stage":              job.Stage,
		"variants_completed": job.VariantsCompleted,
		"queued_at":          job.QueuedAt,
		"started_at":         job.StartedAt,
	}

	if job.Status == "completed" {
		res["completed_at"] = job.CompletedAt
		variants, err := app.models.Variants.GetAllForImage(job.ImageID)
		if err == nil {
			varList := make([]map[string]any, 0, len(variants))
			for _, v := range variants {
				varList = append(varList, map[string]any{
					"name":   v.Name,
					"width":  v.Width,
					"height": v.Height,
					"url":    fmt.Sprintf("/v1/images/%s/variants/%s", v.ImageID, v.Name),
				})
			}
			res["variants"] = varList
		}
	} else if job.Status == "failed" {
		res["failed_at"] = job.FailedAt
		res["error"] = job.ErrorMessage
	}

	// Wrap in "job" key to match the existing frontend expectation: `const job = data.job;`
	return map[string]any{"job": res}
}