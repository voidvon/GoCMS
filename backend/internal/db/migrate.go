package db

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ImportReport struct {
	Tables map[string]int64
	Total  int64
}

func ImportAccess(ctx context.Context, accessPath, sqlitePath string, force bool) (ImportReport, error) {
	if _, err := os.Stat(accessPath); err != nil {
		return ImportReport{}, fmt.Errorf("access database: %w", err)
	}
	if _, err := exec.LookPath("mdb-export"); err != nil {
		return ImportReport{}, fmt.Errorf("mdb-export is required; install mdbtools first: %w", err)
	}
	if _, err := os.Stat(sqlitePath); err == nil && !force {
		return ImportReport{}, fmt.Errorf("sqlite database already exists: %s (use -force to replace it)", sqlitePath)
	} else if err != nil && !os.IsNotExist(err) {
		return ImportReport{}, fmt.Errorf("check sqlite database: %w", err)
	}

	databaseDir := filepath.Dir(sqlitePath)
	if err := os.MkdirAll(databaseDir, 0o755); err != nil {
		return ImportReport{}, fmt.Errorf("create sqlite directory: %w", err)
	}
	temporary, err := os.CreateTemp(databaseDir, ".bilvie-import-*.db")
	if err != nil {
		return ImportReport{}, fmt.Errorf("create temporary sqlite database: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return ImportReport{}, fmt.Errorf("close temporary sqlite database: %w", err)
	}
	defer os.Remove(temporaryPath)

	database, err := Open(temporaryPath)
	if err != nil {
		return ImportReport{}, err
	}
	closeDatabase := true
	defer func() {
		if closeDatabase {
			database.Close()
		}
	}()

	if err := CreateSchema(ctx, database); err != nil {
		return ImportReport{}, err
	}
	report := ImportReport{Tables: make(map[string]int64, len(AccessTables))}
	for _, table := range AccessTables {
		count, err := importTable(ctx, database, accessPath, table)
		if err != nil {
			return ImportReport{}, fmt.Errorf("import %s: %w", table.Name, err)
		}
		report.Tables[table.Name] = count
		report.Total += count
	}
	if err := NormalizeImagePaths(ctx, database); err != nil {
		return ImportReport{}, fmt.Errorf("normalize image paths: %w", err)
	}

	if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return ImportReport{}, fmt.Errorf("checkpoint sqlite database: %w", err)
	}
	if err := database.Close(); err != nil {
		return ImportReport{}, fmt.Errorf("close sqlite database: %w", err)
	}
	closeDatabase = false

	if force {
		if err := os.Remove(sqlitePath); err != nil && !os.IsNotExist(err) {
			return ImportReport{}, fmt.Errorf("replace existing sqlite database: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, sqlitePath); err != nil {
		return ImportReport{}, fmt.Errorf("install sqlite database: %w", err)
	}
	return report, nil
}

func importTable(ctx context.Context, database *sql.DB, accessPath string, table Table) (int64, error) {
	command := exec.CommandContext(ctx, "mdb-export", "-H", "-D", "%Y-%m-%d", "-T", "%Y-%m-%d %H:%M:%S", accessPath, table.Name)
	output, err := command.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("open mdb-export output: %w", err)
	}
	if err := command.Start(); err != nil {
		return 0, fmt.Errorf("start mdb-export: %w", err)
	}

	placeholders := make([]string, len(table.Columns))
	quotedColumns := make([]string, len(table.Columns))
	for index, column := range table.Columns {
		placeholders[index] = "?"
		quotedColumns[index] = quoteIdentifier(column.Name)
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdentifier(table.Name), strings.Join(quotedColumns, ", "), strings.Join(placeholders, ", "))

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, insertSQL)
	if err != nil {
		transaction.Rollback()
		return 0, fmt.Errorf("prepare insert: %w", err)
	}

	reader := csv.NewReader(output)
	reader.FieldsPerRecord = -1
	var count int64
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			statement.Close()
			transaction.Rollback()
			return 0, fmt.Errorf("read CSV row %d: %w", count+1, readErr)
		}
		if len(record) != len(table.Columns) {
			statement.Close()
			transaction.Rollback()
			return 0, fmt.Errorf("row %d has %d columns, expected %d", count+1, len(record), len(table.Columns))
		}
		values := make([]any, len(record))
		for index, value := range record {
			values[index], err = importValue(value, table.Columns[index])
			if err != nil {
				statement.Close()
				transaction.Rollback()
				return 0, fmt.Errorf("row %d column %s: %w", count+1, table.Columns[index].Name, err)
			}
		}
		if _, err := statement.ExecContext(ctx, values...); err != nil {
			statement.Close()
			transaction.Rollback()
			return 0, fmt.Errorf("insert row %d: %w", count+1, err)
		}
		count++
	}
	if err := statement.Close(); err != nil {
		transaction.Rollback()
		return 0, fmt.Errorf("close insert statement: %w", err)
	}
	if err := command.Wait(); err != nil {
		transaction.Rollback()
		return 0, fmt.Errorf("mdb-export %s: %w", table.Name, err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit %s: %w", table.Name, err)
	}
	return count, nil
}

func importValue(value string, column Column) (any, error) {
	if value == "" {
		return nil, nil
	}
	if column.Type == ColumnInteger {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	}
	if column.Type == ColumnDateTime {
		if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
			return parsed.Format("2006-01-02 15:04:05"), nil
		}
	}
	return value, nil
}
