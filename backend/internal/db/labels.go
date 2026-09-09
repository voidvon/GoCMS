package db

import (
	"context"
	"database/sql"
	"fmt"
)

// NormalizeLegacyLabels converts the small set of category/content tags used
// by the imported theme fragments into the generic tag vocabulary. It is an
// import step; the publisher never resolves the old names.
func NormalizeLegacyLabels(ctx context.Context, database *sql.DB) error {
	for _, replacement := range [][2]string{
		{"#HOPE_ProductsCat2()#", "#categories_plain()#"},
		{"#HOPE_ProductsCat()#", "#categories()#"},
	} {
		if _, err := database.ExecContext(ctx, `
			UPDATE "benming_ch_cuslabel"
			SET "lcontent" = REPLACE("lcontent", ?, ?)
			WHERE instr("lcontent", ?) > 0`, replacement[0], replacement[1], replacement[0]); err != nil {
			return fmt.Errorf("replace legacy label %s: %w", replacement[0], err)
		}
	}
	return nil
}
