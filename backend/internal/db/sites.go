package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
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
	prefix := ""
	if strings.HasPrefix(value, "*.") {
		prefix = "*."
		value = value[2:]
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
			return prefix + parsed.Hostname()
		}
	}
	if parsed, err := url.Parse("//" + value); err == nil && parsed.Hostname() != "" {
		return prefix + parsed.Hostname()
	}
	host := strings.Split(value, "/")[0]
	host = strings.Split(host, ":")[0]
	return prefix + strings.TrimSpace(host)
}

func matchHostPattern(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "" || host == "" {
		return false
	}
	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // e.g. ".example.com"
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return false
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
		WHERE "domain" = ?
		LIMIT 1`, host)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			return scanSite(rows)
		}
	}

	// 2. Scan all sites to check aliases (exact match)
	sites, err := ListSites(ctx, query)
	if err != nil {
		return nil, err
	}
	for _, s := range sites {
		for _, alias := range s.Aliases {
			if NormalizeDomain(alias) == host {
				match := s
				return &match, nil
			}
		}
	}

	// 3. Scan wildcard domain or aliases (*.domain.com)
	for _, s := range sites {
		if matchHostPattern(s.Domain, host) {
			match := s
			return &match, nil
		}
		for _, alias := range s.Aliases {
			if matchHostPattern(alias, host) {
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
	if s.Domain != "" {
		siteURL := "https://" + s.Domain
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO "`+siteSettingsTable+`" ("site_id", "key", "value", "updated_at")
			VALUES (?, 'site_url', ?, CURRENT_TIMESTAMP)`, siteID, siteURL); err != nil {
			return nil, err
		}
	}

	var tplCount int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'gocms_template_assignment'`).Scan(&tplCount)
	if tplCount > 0 {
		_, _ = tx.ExecContext(ctx, `
			INSERT INTO "gocms_template_assignment" ("site_id", "key", "template_path")
			VALUES (?, 'message', 'msg.html'), (?, 'search', 'search.html')
			ON CONFLICT ("site_id", "key") DO NOTHING`, siteID, siteID)
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

	var existingIsDefault int
	if err := database.QueryRowContext(ctx, `SELECT "is_default" FROM "`+SiteTable+`" WHERE "id" = ?`, s.ID).Scan(&existingIsDefault); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	if (s.IsDefault || existingIsDefault == 1) && s.Status == "disabled" {
		return errors.New("默认站点状态必须为正常，不允许停用")
	}
	if existingIsDefault == 1 && !s.IsDefault {
		return errors.New("系统必须保留一个默认站点，请先将其他站点设为默认站点")
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE "`+SiteTable+`" SET "is_default" = 0 WHERE "id" <> ?`, s.ID); err != nil {
			return fmt.Errorf("reset other default sites: %w", err)
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

	// Keep site_name setting in sync
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO "`+siteSettingsTable+`" ("site_id", "key", "value", "updated_at")
		VALUES (?, 'site_name', ?, CURRENT_TIMESTAMP)
		ON CONFLICT("site_id", "key") DO UPDATE SET "value" = excluded."value", "updated_at" = CURRENT_TIMESTAMP`,
		s.ID, s.Name)

	// If site_url is empty in settings and domain is provided, populate it
	if s.Domain != "" {
		var existingURL string
		_ = tx.QueryRowContext(ctx, `SELECT "value" FROM "`+siteSettingsTable+`" WHERE "site_id" = ? AND "key" = 'site_url'`, s.ID).Scan(&existingURL)
		if strings.TrimSpace(existingURL) == "" {
			_, _ = tx.ExecContext(ctx, `
				INSERT INTO "`+siteSettingsTable+`" ("site_id", "key", "value", "updated_at")
				VALUES (?, 'site_url', ?, CURRENT_TIMESTAMP)
				ON CONFLICT("site_id", "key") DO UPDATE SET "value" = excluded."value", "updated_at" = CURRENT_TIMESTAMP`,
				s.ID, "https://"+s.Domain)
		}
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

	// Cascade delete dependent sub-records first
	dependentDeletes := []struct {
		stmt string
		args []any
	}{
		{
			stmt: `DELETE FROM "gocms_media_ref" WHERE "content_id" IN (SELECT "id" FROM "gocms_content" WHERE "site_id" = ?) OR "media_id" IN (SELECT "id" FROM "gocms_media" WHERE "site_id" = ?)`,
			args: []any{id, id},
		},
		{
			stmt: `DELETE FROM "gocms_content_translation" WHERE "content_id" IN (SELECT "id" FROM "gocms_content" WHERE "site_id" = ?)`,
			args: []any{id},
		},
		{
			stmt: `DELETE FROM "gocms_category_translation" WHERE "category_id" IN (SELECT "id" FROM "gocms_category" WHERE "site_id" = ?)`,
			args: []any{id},
		},
		{
			stmt: `DELETE FROM "gocms_user_group_member" WHERE "group_id" IN (SELECT "id" FROM "gocms_user_group" WHERE "site_id" = ?) OR "user_id" IN (SELECT "id" FROM "gocms_user" WHERE "site_id" = ?)`,
			args: []any{id, id},
		},
		{
			stmt: `DELETE FROM "gocms_user_session" WHERE "user_id" IN (SELECT "id" FROM "gocms_user" WHERE "site_id" = ?)`,
			args: []any{id},
		},
	}
	for _, item := range dependentDeletes {
		if _, err := tx.ExecContext(ctx, item.stmt, item.args...); err != nil {
			return fmt.Errorf("delete site dependent records: %w", err)
		}
	}

	// Cascade delete direct site-scoped tables
	tables := []string{
		"gocms_category",
		"gocms_content",
		"gocms_media",
		"gocms_message",
		"gocms_site_setting",
		"gocms_template_assignment",
		"gocms_template_label",
		"gocms_user_group",
		"gocms_admin_operation",
	}
	for _, t := range tables {
		var count int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t).Scan(&count)
		if count > 0 {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE "site_id" = ?`, t), id); err != nil {
				return fmt.Errorf("delete site records from %s: %w", t, err)
			}
		}
	}

	// Clean up users associated with the deleted site
	var userTableCount int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'gocms_user'`).Scan(&userTableCount)
	if userTableCount > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, site_id, COALESCE(site_ids, '[]') FROM gocms_user`)
		if err == nil {
			type userCleanUp struct {
				uid        int64
				nextSiteID int64
				newSiteIDs string
				shouldDel  bool
			}
			var toClean []userCleanUp
			for rows.Next() {
				var uid, uSiteID int64
				var rawSIDs string
				if err := rows.Scan(&uid, &uSiteID, &rawSIDs); err == nil {
					var ids []int64
					_ = json.Unmarshal([]byte(rawSIDs), &ids)
					hasDeletedSite := (uSiteID == id)
					otherSites := make([]int64, 0, len(ids))
					for _, sid := range ids {
						if sid == id {
							hasDeletedSite = true
						} else {
							otherSites = append(otherSites, sid)
						}
					}
					if hasDeletedSite {
						if len(otherSites) == 0 {
							toClean = append(toClean, userCleanUp{uid: uid, shouldDel: true})
						} else {
							nextSite := otherSites[0]
							newJSON, _ := json.Marshal(otherSites)
							toClean = append(toClean, userCleanUp{uid: uid, nextSiteID: nextSite, newSiteIDs: string(newJSON)})
						}
					}
				}
			}
			rows.Close()
			for _, u := range toClean {
				if u.shouldDel {
					_, _ = tx.ExecContext(ctx, `DELETE FROM gocms_user WHERE id = ?`, u.uid)
					_, _ = tx.ExecContext(ctx, `DELETE FROM gocms_user_group_member WHERE user_id = ?`, u.uid)
					_, _ = tx.ExecContext(ctx, `DELETE FROM gocms_user_session WHERE user_id = ?`, u.uid)
				} else {
					_, _ = tx.ExecContext(ctx, `UPDATE gocms_user SET site_id = ?, site_ids = ? WHERE id = ?`, u.nextSiteID, u.newSiteIDs, u.uid)
				}
			}
		}
	}

	// Clean up admin groups referring to the deleted site
	var grpTableCount int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'gocms_admin_group'`).Scan(&grpTableCount)
	if grpTableCount > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, site_ids, site_permissions FROM gocms_admin_group WHERE site_ids IS NOT NULL OR site_permissions IS NOT NULL`)
		if err == nil {
			type grpUp struct {
				gid       int64
				siteIDs   *string
				sitePerms string
				hasIDs    bool
			}
			var toUpdate []grpUp
			for rows.Next() {
				var gid int64
				var sIDs, sPerms sql.NullString
				if err := rows.Scan(&gid, &sIDs, &sPerms); err == nil {
					changed := false
					var newSIDs *string
					hasIDs := false
					if sIDs.Valid && sIDs.String != "" {
						var ids []int64
						if json.Unmarshal([]byte(sIDs.String), &ids) == nil {
							hasIDs = true
							filtered := make([]int64, 0, len(ids))
							for _, sid := range ids {
								if sid != id {
									filtered = append(filtered, sid)
								} else {
									changed = true
								}
							}
							if changed {
								b, _ := json.Marshal(filtered)
								str := string(b)
								newSIDs = &str
							}
						}
					}
					newSPerms := sPerms.String
					if sPerms.Valid && sPerms.String != "" {
						var permsMap map[string][]string
						if json.Unmarshal([]byte(sPerms.String), &permsMap) == nil {
							key := strconv.FormatInt(id, 10)
							if _, exists := permsMap[key]; exists {
								delete(permsMap, key)
								changed = true
								b, _ := json.Marshal(permsMap)
								newSPerms = string(b)
							}
						}
					}
					if changed {
						toUpdate = append(toUpdate, grpUp{gid, newSIDs, newSPerms, hasIDs})
					}
				}
			}
			rows.Close()
			for _, u := range toUpdate {
				if u.hasIDs && u.siteIDs != nil {
					_, _ = tx.ExecContext(ctx, `UPDATE gocms_admin_group SET site_ids = ?, site_permissions = ? WHERE id = ?`, *u.siteIDs, u.sitePerms, u.gid)
				} else {
					_, _ = tx.ExecContext(ctx, `UPDATE gocms_admin_group SET site_permissions = ? WHERE id = ?`, u.sitePerms, u.gid)
				}
			}
		}
	}


	if _, err := tx.ExecContext(ctx, `DELETE FROM "`+SiteTable+`" WHERE "id" = ?`, id); err != nil {
		return fmt.Errorf("delete site: %w", err)
	}

	return tx.Commit()
}
