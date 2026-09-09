package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder with image.DecodeConfig
	_ "image/png"  // registers the PNG decoder with image.DecodeConfig
	"io"
	"net/http"
	"os"
	"path/filepath"

	"imagelab/internal/data"
)

// uploadImageHandler implements acceptance boundary only:
// validate, store the original, record it. It deliberately does NOT
// create a job or return 202 -- there is no worker yet to own that work
func (app *application) uploadImageHandler(w http.ResponseWriter, r *http.Request) {
	// an oversized request body is rejected before any multipart parsing touches disk or the database.
	r.Body = http.MaxBytesReader(w, r.Body, app.config.upload.maxBytes+512<<10)

	file, header, err := r.FormFile("image")
	if err != nil {
		app.badRequestResponse(w, r, fmt.Errorf("could not read uploaded file: %w", err))
		return
	}
	defer file.Close()

	// Read the whole file into memory. This is safe because we have already validated the size of the request body
	buf, err := io.ReadAll(file)
	if err != nil {
		app.badRequestResponse(w, r, fmt.Errorf("could not read uploaded file: %w", err))
		return
	}

	if int64(len(buf)) > app.config.upload.maxBytes {
		app.badRequestResponse(w, r, errors.New("image exceeds the 10 MB upload limit"))
		return
	}

	// authoritative validation happens here, server-side, by actually decoding the bytes 
	format, err := detectImageFormat(buf)
	if err != nil {
		app.badRequestResponse(w, r, errors.New("unsupported or undecodable image; only JPEG and PNG are accepted"))
		return
	}

	// the stored filename is generated here, never derived from header.Filename.
	storedName, ext, err := generateStoredFilename(format)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	storedFilename := storedName + ext

	destPath := filepath.Join(app.config.storage.root, "originals", storedFilename)
	if err := os.WriteFile(destPath, buf, 0o644); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	img := &data.Image{
		OriginalFilename: header.Filename, // display metadata only, never a filesystem path
		StoredFilename:   storedFilename,
		MediaType:        "image/" + format,
		SizeBytes:        int64(len(buf)),
	}

	if err := app.models.Images.Insert(img); err != nil {
		// if the durable record can't be created, clean up the
		// file we just wrote so a failed upload doesn't leave an orphaned
		// original on disk with no corresponding database row.
		_ = os.Remove(destPath)
		app.serverErrorResponse(w, r, err)
		return
	}

	app.logger.Info("image stored",
		"image_id", img.ID,
		"stored_filename", img.StoredFilename,
		"size_bytes", img.SizeBytes,
	)

	// response shape is intentionally NOT the final 202 contract
	// as there is no job for a job_id/status_url to point to
	// yet. This changes in Week 2 once Jobs.Insert and the worker exist.
	app.writeJSON(w, http.StatusCreated, envelope{
		"image_id":          img.ID,
		"original_filename": img.OriginalFilename,
		"stored_filename":   img.StoredFilename,
		"media_type":        img.MediaType,
		"size_bytes":        img.SizeBytes,
	}, nil)
}

// detectImageFormat decodes just enough of buf to confirm it is a real,
// decodable JPEG or PNG -- not merely a file whose name or declared
// Content-Type happens to match.
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

// generateStoredFilename returns a random, server-controlled base name and
// the correct extension for the detected format
func generateStoredFilename(format string) (name string, ext string, err error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	ext = ".jpg"
	if format == "png" {
		ext = ".png"
	}
	return hex.EncodeToString(b), ext, nil
}
