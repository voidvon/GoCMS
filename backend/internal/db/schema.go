package db

import (
	"context"
	"database/sql"
	"fmt"

	"gocms/internal/templateconfig"
	"gocms/internal/templatelabel"
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
		{name: "languages", fn: EnsureLanguages},
		{name: "admin users", fn: EnsureAdminUsers},
		{name: "categories", fn: EnsureUnifiedCategories},
		{name: "models", fn: EnsureModels},
		{name: "content", fn: EnsureContent},
		{name: "media", fn: EnsureMedia},
		{name: "messages", fn: EnsureMessages},
		{name: "api keys", fn: EnsureApiKeys},
		{name: "template assignments", fn: templateconfig.Ensure},
		{name: "template labels", fn: templatelabel.Ensure},
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
