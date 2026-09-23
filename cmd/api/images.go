package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder used by image.DecodeConfig
	_ "image/png"  // registers the PNG decoder used by image.DecodeConfig
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"imagelab/internal/data"
)

// acceptedMediaTypes is the single source of truth for what ImageLab
// accepts. The upload handler enforces it and the constraints endpoint
// reports it, so the disclaimer shown in the browser can never drift out
// of sync with what the server actually allows.
var acceptedMediaTypes = []string{"image/jpeg", "image/png"}

// uploadConstraintsHandler lets the frontend render its "JPEG or PNG, up
// to 10 MB" disclaimer from real server configuration instead of a
// hardcoded string that someone forgets to update.
func (app *application) uploadConstraintsHandler(w http.ResponseWriter, r *http.Request) {
	env := envelope{
		"accepted_media_types": acceptedMediaTypes,
		"accepted_extensions":  []string{".jpg", ".jpeg", ".png"},
		"max_bytes":            app.config.upload.maxBytes,
	}
	if err := app.writeJSON(w, http.StatusOK, env, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

// uploadImageHandler implements Week 2 requirement:
// validate the upload, store the original, insert a durable queued job row,
// and return 202 Accepted with Location header and job details.
func (app *application) uploadImageHandler(w http.ResponseWriter, r *http.Request) {
	// VAL-01 at the transport layer: an oversized body is cut off before
	// multipart parsing writes anything to disk or touches the database.
	// The extra slack covers multipart boundaries and headers wrapped
	// around the file bytes themselves.
	r.Body = http.MaxBytesReader(w, r.Body, app.config.upload.maxBytes+512<<10)

	file, header, err := r.FormFile("image")
	if err != nil {
		// A MaxBytesError surfaces here rather than at ReadAll, because
		// the multipart parser hits the cap while reading the part.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			app.entityTooLargeResponse(w, r, fmt.Sprintf(
				"image exceeds the %s upload limit", humanBytes(app.config.upload.maxBytes)))
			return
		}
		if errors.Is(err, http.ErrMissingFile) {
			app.badRequestResponse(w, r, errors.New("no image was included in the request"))
			return
		}
		app.badRequestResponse(w, r, errors.New("could not read the uploaded file"))
		return
	}
	defer file.Close()

	// Reading the whole file into memory is safe here precisely because
	// MaxBytesReader already bounded it. Buffering also lets us
	// decode-validate and then write the same bytes without needing to
	// rewind a stream we cannot rewind.
	buf, err := io.ReadAll(file)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			app.entityTooLargeResponse(w, r, fmt.Sprintf(
				"image exceeds the %s upload limit", humanBytes(app.config.upload.maxBytes)))
			return
		}
		app.badRequestResponse(w, r, errors.New("could not read the uploaded file"))
		return
	}

	if len(buf) == 0 {
		app.badRequestResponse(w, r, errors.New("the uploaded file is empty"))
		return
	}

	if int64(len(buf)) > app.config.upload.maxBytes {
		app.entityTooLargeResponse(w, r, fmt.Sprintf(
			"image is %s, which exceeds the %s upload limit",
			humanBytes(int64(len(buf))), humanBytes(app.config.upload.maxBytes)))
		return
	}

	// VAL-02: authoritative validation. The server decodes the actual
	// bytes rather than trusting header.Filename's extension or the
	// browser-supplied Content-Type, both of which the client fully
	// controls. A .png that is really a GIF is rejected here.
	format, err := detectImageFormat(buf)
	if err != nil {
		app.unsupportedMediaTypeResponse(w, r, unsupportedTypeMessage(buf))
		return
	}

	// VAL-03: the stored name is generated server-side. Deriving it from
	// header.Filename would let a crafted name like "../../etc/passwd"
	// escape the storage directory.
	storedFilename, err := generateStoredFilename(format)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	destPath := filepath.Join(app.config.storage.root, "originals", storedFilename)
	if err := os.WriteFile(destPath, buf, 0o644); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	img := &data.Image{
		OriginalFilename: header.Filename, // VAL-04: display metadata only
		StoredFilename:   storedFilename,
		MediaType:        "image/" + format,
		SizeBytes:        int64(len(buf)),
	}

	if err := app.models.Images.Insert(img); err != nil {
		// VAL-05: if the durable record cannot be created, remove the file
		// just written so a failed acceptance leaves no orphaned original
		// on disk with no row pointing at it.
		_ = os.Remove(destPath)
		app.serverErrorResponse(w, r, err)
		return
	}

	// Create durable queued job for background worker processing
	job, err := app.models.Jobs.Insert(img.ID)
	if err != nil {
		_ = os.Remove(destPath)
		app.serverErrorResponse(w, r, err)
		return
	}

	statusURL := fmt.Sprintf("/v1/jobs/%s", job.ID)
	headers := make(http.Header)
	headers.Set("Location", statusURL)

	app.logger.Info("image stored and job queued",
		"image_id", img.ID,
		"job_id", job.ID,
		"stored_filename", img.StoredFilename,
		"media_type", img.MediaType,
		"size_bytes", img.SizeBytes,
	)

	// Week 2 response contract: 202 Accepted with status URL location
	if err := app.writeJSON(w, http.StatusAccepted, envelope{
		"image_id":   img.ID,
		"job_id":     job.ID,
		"status":     job.Status,
		"status_url": statusURL,
	}, headers); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

// getJobHandler exposes GET /v1/jobs/{job_id} for short-polling progress.
func (app *application) getJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if jobID == "" {
		app.notFoundResponse(w, r)
		return
	}

	job, err := app.models.Jobs.Get(jobID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	res := envelope{
		"id":         job.ID,
		"image_id":   job.ImageID,
		"status":     job.Status,
		"queued_at":  job.QueuedAt,
		"started_at": job.StartedAt,
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

	if err := app.writeJSON(w, http.StatusOK, envelope{"job": res}, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

// getVariantHandler exposes GET /v1/images/{image_id}/variants/{name}
func (app *application) getVariantHandler(w http.ResponseWriter, r *http.Request) {
	imageID := r.PathValue("image_id")
	variantName := r.PathValue("name")

	if imageID == "" || variantName == "" {
		app.notFoundResponse(w, r)
		return
	}

	variant, err := app.models.Variants.GetByImageAndName(imageID, variantName)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	filePath := filepath.Join(app.config.storage.root, "variants", variant.StoredFilename)
	http.ServeFile(w, r, filePath)
}

// detectImageFormat confirms the bytes really are a decodable JPEG or PNG.
// DecodeConfig reads only the image header, so this costs far less than a
// full decode while still proving the file is genuinely what it claims.
func detectImageFormat(buf []byte) (string, error) {
	_, format, err := image.DecodeConfig(bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	if format != "jpeg" && format != "png" {
		return "", fmt.Errorf("unsupported format: %s", format)
	}
	return format, nil
}

// unsupportedTypeMessage builds a message that tells the user what they
// actually submitted. http.DetectContentType sniffs the magic bytes, so a
// GIF renamed to .png is reported as a GIF -- which is far more useful
// than a generic rejection, and still reveals nothing about the server.
func unsupportedTypeMessage(buf []byte) string {
	sniffed := http.DetectContentType(buf)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = sniffed[:i] // strip charset parameters
	}

	base := "only JPEG and PNG images are accepted"
	switch {
	case sniffed == "application/octet-stream":
		return "this file could not be read as an image; " + base
	case strings.HasPrefix(sniffed, "image/"):
		return fmt.Sprintf("%s images are not supported; %s", strings.ToUpper(strings.TrimPrefix(sniffed, "image/")), base)
	default:
		return fmt.Sprintf("this file looks like %s, not an image; %s", sniffed, base)
	}
}

// generateStoredFilename returns a random, server-controlled filename with
// the extension matching the format the server actually detected -- never
// the extension the client supplied.
func generateStoredFilename(format string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	return hex.EncodeToString(b) + ext, nil
}

// humanBytes formats a byte count for user-facing error messages.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}