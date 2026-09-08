package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if _, err := database.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		database.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		database.Close()
		return nil, fmt.Errorf("configure sqlite foreign keys: %w", err)
	}
	return database, nil
}
