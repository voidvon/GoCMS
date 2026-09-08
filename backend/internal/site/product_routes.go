package site

import (
	"context"
	"database/sql"
	"path"

	"bilvie/internal/routing"
)

func (s *Server) productDetailURL(ctx context.Context, categoryID, productID int64) (string, error) {
	directory := routing.DefaultDetailPath
	pattern := routing.DefaultDetailPattern
	if categoryID > 0 {
		var storedDirectory, storedPattern string
		err := s.database.QueryRowContext(ctx, `
			SELECT COALESCE("DetailPath", ''), COALESCE("DetailFilePattern", '')
			FROM "benming_ch_ProdCat" WHERE "id" = ?`, categoryID).
			Scan(&storedDirectory, &storedPattern)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}
		if storedDirectory != "" {
			directory = storedDirectory
		}
		if storedPattern != "" {
			pattern = storedPattern
		}
	}
	directory, err := routing.NormalizeDirectory(directory)
	if err != nil {
		return "", err
	}
	filename, err := routing.RenderDetailFilename(pattern, productID)
	if err != nil {
		return "", err
	}
	return path.Join("/", directory, filename), nil
}
