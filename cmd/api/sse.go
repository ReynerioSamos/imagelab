package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"imagelab/internal/data"
)

func (app *application) sseJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if jobID == "" {
		app.notFoundResponse(w, r)
		return
	}

	// SSE-02: 404 for unknown job BEFORE streaming.
	job, err := app.models.Jobs.Get(jobID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// SSE-03: headers before body.
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	rc := http.NewResponseController(w)
	// write deadline so Condition A / long jobs are not severed mid-stream.
	_ = rc.SetWriteDeadline(time.Time{})

	ch := globalHub.Subscribe(jobID)
	defer globalHub.Unsubscribe(jobID, ch)

	writeSSEEvent := func(eventName string, payload any) error {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return err
		}
		return rc.Flush() // SSE-06
	}

	// SSE-04: immediate snapshot.
	if err := writeSSEEvent("job-update", app.buildSSEJobPayload(job)); err != nil {
		return
	}
	lastVersion := job.Version

	// SSE-08: liveness heartbeat (NOT a job-update, never changes version).
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()

	for {
		select {
		case <-r.Context().Done(): // SYNC-07: client gone, worker unaffected.
			app.logger.Info("SSE client disconnected", "job_id", jobID)
			return

		case <-hb.C:
			_ = writeSSEEvent("heartbeat", map[string]any{
				"ts": time.Now().UTC().Format(time.RFC3339Nano),
			})

		case <-ch:
			cur, err := app.models.Jobs.Get(jobID) // SYNC-04: re-read DB.
			if err != nil {
				continue
			}
			if cur.Version > lastVersion { // SYNC-05: skip coalesced.
				lastVersion = cur.Version
				if err := writeSSEEvent("job-update", app.buildSSEJobPayload(cur)); err != nil {
					return
				}
				if cur.Status == "completed" || cur.Status == "failed" { // SSE-07
					return
				}
			}
		}
	}
}

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
		if variants, err := app.models.Variants.GetAllForImage(job.ImageID); err == nil {
			varList := make([]map[string]any, 0, len(variants))
			for _, v := range variants {
				varList = append(varList, map[string]any{
					"name": v.Name, "width": v.Width, "height": v.Height,
					"url": fmt.Sprintf("/v1/images/%s/variants/%s", v.ImageID, v.Name),
				})
			}
			res["variants"] = varList
		}
	} else if job.Status == "failed" {
		res["failed_at"] = job.FailedAt
		res["error"] = job.ErrorMessage
	}
	return map[string]any{"job": res}
}