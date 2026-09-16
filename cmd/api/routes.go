package main

import "net/http"

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/healthcheck", app.healthcheckHandler)

	// Tells the browser what the server will actually accept, so the
	// disclaimer shown in the UI comes from one authoritative source
	// rather than being hardcoded separately in JavaScript.
	mux.HandleFunc("GET /v1/upload-constraints", app.uploadConstraintsHandler)

	mux.HandleFunc("POST /v1/images", app.uploadImageHandler)

	// Week 2 adds: GET /v1/jobs/{job_id}
	// Week 2 adds: GET /v1/images/{image_id}/variants/{name}

	// Serves index.html/app.js/style.css from ./frontend so the browser
	// and API share one origin -- no CORS configuration needed.
	mux.Handle("/", http.FileServer(http.Dir("./frontend")))

	return mux
}