package data

import "database/sql"

type Models struct {
	Images ImageModel
	// Jobs and Variants models aren't added yet, once a worker exists
	// to actually claim and act on job rows.
	// Their Go struct shapes are already prepared below in jobs.go and
	// variants.go so the schema and the code agree from the start.
}

func NewModels(db *sql.DB) Models {
	return Models{
		Images: ImageModel{DB: db},
	}
}
