package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const SiteTable = "gocms_site"

var siteCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

type Site struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Code      string   `json:"code"`
	Domain    string   `json:"domain"`
	Aliases   []string `json:"aliases"`
	ThemeID   string   `json:"theme_id"`
	OutputDir string   `json:"output_dir"`
	IsDefault bool     `json:"is_default"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func EnsureSites(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+SiteTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"name" TEXT NOT NULL,
			"code" TEXT NOT NULL UNIQUE,
			"domain" TEXT NOT NULL DEFAULT '',
			"aliases" TEXT NOT NULL DEFAULT '[]',
			"theme_id" TEXT NOT NULL DEFAULT '',
			"output_dir" TEXT NOT NULL DEFAULT '',
			"is_default" INTEGER NOT NULL DEFAULT 0,
			"status" TEXT NOT NULL DEFAULT 'active',
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create site table: %w", err)
	}

	for _, statement := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gocms_site_code ON "` + SiteTable + `" ("code")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_site_domain ON "` + SiteTable + `" ("domain")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_site_default ON "` + SiteTable + `" ("is_default")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create site index: %w", err)
		}
	}

	for name, definition := range map[string]string{
		"output_dir": "TEXT NOT NULL DEFAULT ''",
		"theme_id":   "TEXT NOT NULL DEFAULT ''",
		"status":     "TEXT NOT NULL DEFAULT 'active'",
	} {
		if err := ensureTableColumn(ctx, database, SiteTable, name, definition); err != nil {
			return err
		}
	}

	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+SiteTable+`"`).Scan(&count); err != nil {
		return fmt.Errorf("check sites count: %w", err)
	}
	if count == 0 {
		now := time.Now().Format("2006-01-02 15:04:05")
		var defaultDomain string
		if tableExists(ctx, database, siteSettingsTable) {
			_ = database.QueryRowContext(ctx, `SELECT "value" FROM "`+siteSettingsTable+`" WHERE "key" = 'site_url' LIMIT 1`).Scan(&defaultDomain)
			defaultDomain = NormalizeDomain(defaultDomain)
		}
		if _, err := database.ExecContext(ctx, `
			INSERT INTO "`+SiteTable+`" ("id", "name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at")
			VALUES (1, '默认站点', 'default', ?, '[]', '', '', 1, 'active', ?, ?)`, defaultDomain, now, now); err != nil {
			return fmt.Errorf("seed default site: %w", err)
		}
	}

	return nil
}

func NormalizeDomain(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	if parsed, err := url.Parse("//" + value); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	host := strings.Split(value, "/")[0]
	host = strings.Split(host, ":")[0]
	return strings.TrimSpace(host)
}

func ValidateSiteCode(code string) error {
	code = strings.TrimSpace(strings.ToLower(code))
	if !siteCodePattern.MatchString(code) {
		return errors.New("站点标识必须由 2-64 位小写字母、数字、下划线或短横线组成，且以字母或数字开头")
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSite(scanner rowScanner) (*Site, error) {
	var s Site
	var aliasesJSON string
	var isDefaultInt int
	if err := scanner.Scan(
		&s.ID,
		&s.Name,
		&s.Code,
		&s.Domain,
		&aliasesJSON,
		&s.ThemeID,
		&s.OutputDir,
		&isDefaultInt,
		&s.Status,
		&s.CreatedAt,
		&s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	s.IsDefault = isDefaultInt == 1
	if aliasesJSON != "" && aliasesJSON != "[]" {
		_ = json.Unmarshal([]byte(aliasesJSON), &s.Aliases)
	}
	if s.Aliases == nil {
		s.Aliases = []string{}
	}
	return &s, nil
}

func GetSiteByID(ctx context.Context, query settingsQueryer, id int64) (*Site, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT "id", "name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at"
		FROM "`+SiteTable+`"
		WHERE "id" = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSite(rows)
}

func GetSiteByCode(ctx context.Context, query settingsQueryer, code string) (*Site, error) {
	code = strings.TrimSpace(strings.ToLower(code))
	rows, err := query.QueryContext(ctx, `
		SELECT "id", "name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at"
		FROM "`+SiteTable+`"
		WHERE "code" = ?`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSite(rows)
}

func GetDefaultSite(ctx context.Context, query settingsQueryer) (*Site, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT "id", "name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at"
		FROM "`+SiteTable+`"
		WHERE "is_default" = 1
		ORDER BY "id" ASC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanSite(rows)
	}
	return GetSiteByID(ctx, query, 1)
}

func GetSiteByHost(ctx context.Context, query settingsQueryer, host string) (*Site, error) {
	host = NormalizeDomain(host)
	if host == "" {
		return GetDefaultSite(ctx, query)
	}

	// 1. Direct domain match
	rows, err := query.QueryContext(ctx, `
		SELECT "id", "name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at"
		FROM "`+SiteTable+`"
		WHERE "domain" = ? AND "status" = 'active'
		LIMIT 1`, host)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			return scanSite(rows)
		}
	}

	// 2. Scan all active sites to check aliases
	sites, err := ListSites(ctx, query)
	if err != nil {
		return nil, err
	}
	for _, s := range sites {
		if s.Status != "active" {
			continue
		}
		for _, alias := range s.Aliases {
			if NormalizeDomain(alias) == host {
				match := s
				return &match, nil
			}
		}
	}

	return nil, sql.ErrNoRows
}

func ListSites(ctx context.Context, query settingsQueryer) ([]Site, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT "id", "name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at"
		FROM "`+SiteTable+`"
		ORDER BY "is_default" DESC, "id" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Site
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *site)
	}
	return list, rows.Err()
}

func CreateSite(ctx context.Context, database *sql.DB, s *Site) (*Site, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return nil, errors.New("站点名称不能为空")
	}
	s.Code = strings.TrimSpace(strings.ToLower(s.Code))
	if err := ValidateSiteCode(s.Code); err != nil {
		return nil, err
	}
	s.Domain = NormalizeDomain(s.Domain)
	if s.Status == "" {
		s.Status = "active"
	}
	s.ThemeID = strings.TrimSpace(s.ThemeID)
	s.OutputDir = strings.TrimSpace(strings.Trim(s.OutputDir, "/\\"))
	if s.Aliases == nil {
		s.Aliases = []string{}
	}
	aliasesData, _ := json.Marshal(s.Aliases)

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if s.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE "`+SiteTable+`" SET "is_default" = 0`); err != nil {
			return nil, fmt.Errorf("reset other default sites: %w", err)
		}
	}

	isDefInt := 0
	if s.IsDefault {
		isDefInt = 1
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO "`+SiteTable+`" ("name", "code", "domain", "aliases", "theme_id", "output_dir", "is_default", "status", "created_at", "updated_at")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		s.Name, s.Code, s.Domain, string(aliasesData), s.ThemeID, s.OutputDir, isDefInt, s.Status)
	if err != nil {
		return nil, fmt.Errorf("insert site: %w", err)
	}

	siteID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	s.ID = siteID

	// Seed default site settings for new site
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO "`+siteSettingsTable+`" ("site_id", "key", "value", "updated_at")
		VALUES (?, 'site_name', ?, CURRENT_TIMESTAMP)`, siteID, s.Name); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s, nil
}

func UpdateSite(ctx context.Context, database *sql.DB, s *Site) error {
	if s.ID <= 0 {
		return errors.New("无效的站点 ID")
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return errors.New("站点名称不能为空")
	}
	s.Code = strings.TrimSpace(strings.ToLower(s.Code))
	if err := ValidateSiteCode(s.Code); err != nil {
		return err
	}
	s.Domain = NormalizeDomain(s.Domain)
	if s.Status == "" {
		s.Status = "active"
	}
	s.ThemeID = strings.TrimSpace(s.ThemeID)
	s.OutputDir = strings.TrimSpace(strings.Trim(s.OutputDir, "/\\"))
	if s.Aliases == nil {
		s.Aliases = []string{}
	}
	aliasesData, _ := json.Marshal(s.Aliases)

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE "`+SiteTable+`" SET "is_default" = 0 WHERE "id" <> ?`, s.ID); err != nil {
			return nil
		}
	}

	isDefInt := 0
	if s.IsDefault {
		isDefInt = 1
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE "`+SiteTable+`"
		SET "name" = ?, "code" = ?, "domain" = ?, "aliases" = ?, "theme_id" = ?, "output_dir" = ?, "is_default" = ?, "status" = ?, "updated_at" = CURRENT_TIMESTAMP
		WHERE "id" = ?`,
		s.Name, s.Code, s.Domain, string(aliasesData), s.ThemeID, s.OutputDir, isDefInt, s.Status, s.ID)
	if err != nil {
		return fmt.Errorf("update site: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return sql.ErrNoRows
	}

	return tx.Commit()
}

func DeleteSite(ctx context.Context, database *sql.DB, id int64) error {
	if id <= 1 {
		return errors.New("默认站点不允许删除")
	}

	var isDefault int
	if err := database.QueryRowContext(ctx, `SELECT "is_default" FROM "`+SiteTable+`" WHERE "id" = ?`, id).Scan(&isDefault); err != nil {
		return err
	}
	if isDefault == 1 {
		return errors.New("默认站点不允许删除")
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Cascade delete site-scoped data
	tables := []string{
		"gocms_category",
		"gocms_content",
		"gocms_message",
		"gocms_site_setting",
		"gocms_template_assignment",
		"gocms_template_label",
		"gocms_site_member",
		"gocms_user_group",
	}
	for _, t := range tables {
		var count int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t).Scan(&count)
		if count > 0 {
			_, _ = tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE "site_id" = ?`, t), id)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM "`+SiteTable+`" WHERE "id" = ?`, id); err != nil {
		return fmt.Errorf("delete site: %w", err)
	}

	return tx.Commit()
}
