package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Image records one accepted upload. The original filename is kept purely
// as display metadata; StoredFilename is the server-generated name that
// actually exists on disk, which is what prevents a hostile filename from
// ever reaching the filesystem.
type Image struct {
	// ID is a UUID string rather than an integer: it appears in
	// client-facing URLs, so it must not be enumerable.
	ID               string    `json:"id"`
	OriginalFilename string    `json:"original_filename"`
	StoredFilename   string    `json:"stored_filename"`
	MediaType        string    `json:"media_type"`
	SizeBytes        int64     `json:"size_bytes"`
	CreatedAt        time.Time `json:"created_at"`
}

type ImageModel struct {
	DB *sql.DB
}

func (m ImageModel) Insert(img *Image) error {
	// Generate UUID v7 for image ID
	imageID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	img.ID = imageID.String()

	query := `
		INSERT INTO images (id, original_filename, stored_filename, media_type, size_bytes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at`

	args := []any{
		img.ID,
		img.OriginalFilename,
		img.StoredFilename,
		img.MediaType,
		img.SizeBytes,
	}

	return m.DB.QueryRow(query, args...).Scan(&img.CreatedAt)
}

func (m ImageModel) Get(id string) (*Image, error) {
	query := `SELECT id, original_filename, stored_filename, media_type, size_bytes, created_at
		FROM images WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var img Image
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&img.ID, &img.OriginalFilename, &img.StoredFilename, &img.MediaType, &img.SizeBytes, &img.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &img, nil
}