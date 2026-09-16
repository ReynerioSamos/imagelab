package data

import "time"

// Variant is one generated output (thumbnail, preview, or display) for an
// image. Width/Height record the ACTUAL produced dimensions, which for
// preview and display are usually smaller than the maximum bounds because
// aspect ratio is preserved (IMG-02, IMG-04).
//
// StoredFilename and SizeBytes are json:"-" because the client never
// needs them: variants are served through a known route by name, never by
// path, so exposing the on-disk filename would only widen the attack
// surface.
type Variant struct {
	ID             string    `json:"id"`
	ImageID        string    `json:"image_id"`
	Name           string    `json:"name"`
	StoredFilename string    `json:"-"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	SizeBytes      int64     `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}