package db

import (
	"context"
	"database/sql"
	"fmt"

	"gocms/internal/routing"
)

// The migration marker and rewritten filenames are committed in the same
// transaction so a retry never applies a category pattern twice.
func migrateDetailFilenames(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_schema_migration (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gocms_schema_migration (name) VALUES ('content-detail-filenames')`)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return tx.Commit()
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT content.id, COALESCE(content.route_key, ''), COALESCE(category.detail_file_pattern, '')
		FROM gocms_content content
		LEFT JOIN gocms_category category ON category.id = content.category_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type change struct {
		id       int64
		filename string
	}
	var changes []change
	for rows.Next() {
		var id int64
		var routeKey, pattern string
		if err := rows.Scan(&id, &routeKey, &pattern); err != nil {
			return err
		}
		filename := routing.LegacyDetailFilename(pattern, routeKey)
		if clean, err := routing.NormalizeDetailFilename(filename); err != nil || clean != filename {
			return fmt.Errorf("内容 %d 的旧详情文件名无法安全迁移: %q", id, filename)
		}
		if filename != routeKey {
			changes = append(changes, change{id: id, filename: filename})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range changes {
		if _, err := tx.ExecContext(ctx, `UPDATE gocms_content SET route_key = ? WHERE id = ?`, item.filename, item.id); err != nil {
			return fmt.Errorf("迁移内容 %d 的详情文件名: %w", item.id, err)
		}
	}
	return tx.Commit()
}
