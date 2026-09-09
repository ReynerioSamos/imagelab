package main

import "net/http"

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	// Only routes needed for first check, other routes will be made for other weekly checks like the GET for polling
	mux.HandleFunc("GET /v1/healthcheck", app.healthcheckHandler)
	mux.HandleFunc("POST /v1/images", app.uploadImageHandler)


	// Serves index.html/app.js/style.css from ./frontend so the browser
	// and API share one origin
	mux.Handle("/", http.FileServer(http.Dir("./frontend")))

	return mux
}
