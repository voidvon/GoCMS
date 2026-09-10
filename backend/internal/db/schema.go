package db

import (
	"context"
	"database/sql"
	"fmt"

	"gocms/internal/templateconfig"
)

// CreateSchema creates the database used by the running CMS. Importers for
// external systems have their own schema adapters and are not part of this
// runtime model.
func CreateSchema(ctx context.Context, database *sql.DB) error {
	ensurers := []struct {
		name string
		fn   func(context.Context, *sql.DB) error
	}{
		{name: "site settings", fn: EnsureSiteSettings},
		{name: "admin users", fn: EnsureAdminUsers},
		{name: "categories", fn: EnsureUnifiedCategories},
		{name: "content", fn: EnsureContent},
		{name: "messages", fn: EnsureMessages},
		{name: "template assignments", fn: templateconfig.Ensure},
	}
	for _, ensure := range ensurers {
		if err := ensure.fn(ctx, database); err != nil {
			return fmt.Errorf("ensure %s: %w", ensure.name, err)
		}
	}
	return nil
}

// EnsureTemplateAssignments is a convenience for commands that initialize
// only the template binding table.
func EnsureTemplateAssignments(ctx context.Context, database *sql.DB) error {
	return templateconfig.Ensure(ctx, database)
}
