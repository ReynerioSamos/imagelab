package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

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

type VariantModel struct {
	DB *sql.DB
}

func (m VariantModel) Insert(v *Variant) error {
	// Generate UUID v7 for variant ID
	variantID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	v.ID = variantID.String()

	query := `
		INSERT INTO variants (id, image_id, name, stored_filename, width, height, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`

	args := []any{
		v.ID,
		v.ImageID,
		v.Name,
		v.StoredFilename,
		v.Width,
		v.Height,
		v.SizeBytes,
	}

	return m.DB.QueryRow(query, args...).Scan(&v.CreatedAt)
}

func (m VariantModel) GetByImageAndName(imageID, name string) (*Variant, error) {
	query := `
		SELECT id, image_id, name, stored_filename, width, height, size_bytes, created_at
		FROM variants
		WHERE image_id = $1 AND name = $2`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var v Variant
	err := m.DB.QueryRowContext(ctx, query, imageID, name).Scan(
		&v.ID, &v.ImageID, &v.Name, &v.StoredFilename, &v.Width, &v.Height, &v.SizeBytes, &v.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	return &v, nil
}

func (m VariantModel) GetAllForImage(imageID string) ([]*Variant, error) {
	query := `
		SELECT id, image_id, name, stored_filename, width, height, size_bytes, created_at
		FROM variants
		WHERE image_id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var variants []*Variant
	for rows.Next() {
		var v Variant
		if err := rows.Scan(&v.ID, &v.ImageID, &v.Name, &v.StoredFilename, &v.Width, &v.Height, &v.SizeBytes, &v.CreatedAt); err != nil {
			return nil, err
		}
		variants = append(variants, &v)
	}

	return variants, rows.Err()
}