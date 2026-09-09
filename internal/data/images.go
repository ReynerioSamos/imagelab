package data

import (
	"context"
	"database/sql"
	"time"
)

// model to save the image that will be manipulated in the imagelab app, saves original name but makes own unique name to prevent injection attacks and also collisions
type Image struct {
	ID               int64     `json:"id"`
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
	query := `INSERT INTO images (original_filename, stored_filename, media_type, size_bytes)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query,
		img.OriginalFilename, img.StoredFilename, img.MediaType, img.SizeBytes,
	).Scan(&img.ID, &img.CreatedAt)
}

func (m ImageModel) Get(id int64) (*Image, error) {
	query := `SELECT id, original_filename, stored_filename, media_type, size_bytes, created_at
		FROM images WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var img Image
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&img.ID, &img.OriginalFilename, &img.StoredFilename, &img.MediaType, &img.SizeBytes, &img.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &img, nil
}
