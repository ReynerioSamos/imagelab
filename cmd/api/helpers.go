package main

import (
	"encoding/json"
	"net/http"
)

type envelope map[string]any

func (app *application) writeJSON(w http.ResponseWriter, status int, data envelope, headers http.Header) error {
	js, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return err
	}
	js = append(js, '\n')

	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)
	return nil
}

// Handler for reprocessing a failed job using existing image reference
func (app *application) reprocessJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if jobID == "" {
		app.badRequestResponse(w, r, nil)
		return
	}

	// 1. Fetch existing job to retrieve its image_id
	existingJob, err := app.models.Jobs.Get(jobID)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	// 2. Insert a new job referencing the SAME image_id
	newJob, err := app.models.Jobs.Insert(existingJob.ImageID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// 3. Return status payload to frontend
	env := envelope{
		"job_id":     newJob.ID,
		"status_url": "/v1/jobs/" + newJob.ID,
	}

	if err := app.writeJSON(w, http.StatusAccepted, env, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}