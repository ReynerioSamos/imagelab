package data

import "database/sql"

// DB is exposed on Models so callers that need to coordinate across mutliple tables
// can open their own tx rather than whetever model their own method exposes.

type Models struct {
	DB		 *sql.DB
	Images   ImageModel
	Jobs     JobModel
	Variants VariantModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		DB:		  db,
		Images:   ImageModel{DB: db},
		Jobs:     JobModel{DB: db},
		Variants: VariantModel{DB: db},
	}
}