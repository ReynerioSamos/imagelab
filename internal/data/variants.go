package data

import "time"

// one row per generated thumbnail, preview, or display image. Populated by the
// worker when made; the shape is prepared now for the same reason
// as Job model.
type Variant struct {
	ID             int64     `json:"id"`
	ImageID        int64     `json:"image_id"`
	Name           string    `json:"name"`
	StoredFilename string    `json:"-"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	SizeBytes      int64     `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}
