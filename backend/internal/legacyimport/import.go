package legacyimport

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

	"gocms/internal/db"
)

type ImportReport struct {
	Tables map[string]int64
	Total  int64
}

// ImportAccess is the only entry point that understands the old Access
// schema. It uses temporary source tables while converting data and removes
// them before installing the resulting runtime database.
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
	temporary, err := os.CreateTemp(databaseDir, ".gocms-import-*.db")
	if err != nil {
		return ImportReport{}, fmt.Errorf("create temporary sqlite database: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return ImportReport{}, fmt.Errorf("close temporary sqlite database: %w", err)
	}
	defer os.Remove(temporaryPath)

	database, err := db.Open(temporaryPath)
	if err != nil {
		return ImportReport{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = database.Close()
		}
	}()
	if err := db.CreateSchema(ctx, database); err != nil {
		return ImportReport{}, err
	}

	report := ImportReport{Tables: make(map[string]int64, len(Tables))}
	for _, table := range Tables {
		count, err := importTable(ctx, database, accessPath, table)
		if err != nil {
			return ImportReport{}, fmt.Errorf("import %s: %w", table.Name, err)
		}
		report.Tables[table.Name] = count
		report.Total += count
	}
	if err := migrateRows(ctx, database); err != nil {
		return ImportReport{}, err
	}
	if err := removeSourceTables(ctx, database); err != nil {
		return ImportReport{}, err
	}
	if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return ImportReport{}, fmt.Errorf("checkpoint sqlite database: %w", err)
	}
	if err := database.Close(); err != nil {
		return ImportReport{}, fmt.Errorf("close sqlite database: %w", err)
	}
	closed = true

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

func importTable(ctx context.Context, database *sql.DB, accessPath string, table table) (int64, error) {
	if _, err := database.ExecContext(ctx, table.createSQL()); err != nil {
		return 0, fmt.Errorf("create source table: %w", err)
	}
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
		quotedColumns[index] = quoteIdentifier(column.name)
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdentifier(table.Name), strings.Join(quotedColumns, ", "), strings.Join(placeholders, ", "))

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		_ = command.Wait()
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = transaction.Rollback()
		_ = command.Wait()
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
			_ = statement.Close()
			_ = transaction.Rollback()
			_ = command.Wait()
			return 0, fmt.Errorf("read CSV row %d: %w", count+1, readErr)
		}
		if len(record) != len(table.Columns) {
			_ = statement.Close()
			_ = transaction.Rollback()
			_ = command.Wait()
			return 0, fmt.Errorf("row %d has %d columns, expected %d", count+1, len(record), len(table.Columns))
		}
		values := make([]any, len(record))
		for index, value := range record {
			values[index], err = importValue(value, table.Columns[index])
			if err != nil {
				_ = statement.Close()
				_ = transaction.Rollback()
				_ = command.Wait()
				return 0, fmt.Errorf("row %d column %s: %w", count+1, table.Columns[index].name, err)
			}
		}
		if _, err := statement.ExecContext(ctx, values...); err != nil {
			_ = statement.Close()
			_ = transaction.Rollback()
			_ = command.Wait()
			return 0, fmt.Errorf("insert row %d: %w", count+1, err)
		}
		count++
	}
	if err := statement.Close(); err != nil {
		_ = transaction.Rollback()
		_ = command.Wait()
		return 0, fmt.Errorf("close insert statement: %w", err)
	}
	if err := command.Wait(); err != nil {
		_ = transaction.Rollback()
		return 0, fmt.Errorf("mdb-export %s: %w", table.Name, err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit %s: %w", table.Name, err)
	}
	return count, nil
}

func importValue(value string, column column) (any, error) {
	if value == "" {
		return nil, nil
	}
	if column.typeOf == columnInteger {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	}
	if column.typeOf == columnDateTime {
		if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
			return parsed.Format("2006-01-02 15:04:05"), nil
		}
	}
	return value, nil
}

func readRows(ctx context.Context, database *sql.DB, tableName string) ([]sourceRow, error) {
	rows, err := database.QueryContext(ctx, `SELECT * FROM `+quoteIdentifier(tableName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", tableName, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read %s columns: %w", tableName, err)
	}
	items := make([]sourceRow, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan %s: %w", tableName, err)
		}
		item := sourceRow{}
		for index, column := range columns {
			if values[index] == nil {
				item[strings.ToLower(column)] = ""
			} else if value, ok := values[index].([]byte); ok {
				item[strings.ToLower(column)] = string(value)
			} else {
				item[strings.ToLower(column)] = fmt.Sprint(values[index])
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", tableName, err)
	}
	return items, nil
}

func removeSourceTables(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin source cleanup: %w", err)
	}
	defer transaction.Rollback()
	for _, table := range Tables {
		if _, err := transaction.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdentifier(table.Name)); err != nil {
			return fmt.Errorf("remove source table %s: %w", table.Name, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit source cleanup: %w", err)
	}
	return nil
}
