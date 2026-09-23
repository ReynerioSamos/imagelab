package main

import "net/http"

func (app *application) logError(r *http.Request, err error) {
	app.logger.Error(err.Error(), "method", r.Method, "uri", r.URL.RequestURI())
}

func (app *application) errorResponse(w http.ResponseWriter, r *http.Request, status int, message any) {
	env := envelope{"error": message}
	if err := app.writeJSON(w, status, env, nil); err != nil {
		app.logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// serverErrorResponse never leaks the underlying error, a stack trace, or
// an internal path to the client (Section 13, Minimum Safeguards) -- the
// real error goes only to the log.
func (app *application) serverErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	app.logError(r, err)
	app.errorResponse(w, r, http.StatusInternalServerError, "the server encountered a problem and could not process your request")
}

func (app *application) badRequestResponse(w http.ResponseWriter, r *http.Request, err error) {
	app.errorResponse(w, r, http.StatusBadRequest, err.Error())
}

// unsupportedMediaTypeResponse is used when the upload is well-formed but
// is not a format ImageLab accepts. 415 is more precise than 400 here and
// matches the API contract table in the README.
func (app *application) unsupportedMediaTypeResponse(w http.ResponseWriter, r *http.Request, message string) {
	app.errorResponse(w, r, http.StatusUnsupportedMediaType, message)
}

// entityTooLargeResponse distinguishes "too big" from "wrong type" so the
// browser can show the user which rule they actually broke.
func (app *application) entityTooLargeResponse(w http.ResponseWriter, r *http.Request, message string) {
	app.errorResponse(w, r, http.StatusRequestEntityTooLarge, message)
}

func (app *application) notFoundResponse(w http.ResponseWriter, r *http.Request) {
	app.errorResponse(w, r, http.StatusNotFound, "the requested resource could not be found")
}