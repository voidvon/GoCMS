package legacyimport

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// MigrateExistingSettings repairs a database produced by an earlier import.
// It is a one-time migration step; the runtime publisher never reads the
// source tables after these values are copied into the unified settings table.
func MigrateExistingSettings(ctx context.Context, database *sql.DB) error {
	configRows, err := readRows(ctx, database, "benming_ch_config")
	if err != nil {
		return err
	}
	metaRows, err := readRows(ctx, database, "benming_ch_MetaType")
	if err != nil {
		return err
	}
	companyRows, err := readRows(ctx, database, "benming_ch_Cocat")
	if err != nil {
		return err
	}
	labelRows, err := readRows(ctx, database, "benming_ch_cuslabel")
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings migration: %w", err)
	}
	defer transaction.Rollback()

	if len(configRows) > 0 {
		row := configRows[0]
		values := map[string]string{
			"site_name":        row.text("WebName"),
			"site_url":         row.text("WebUrl"),
			"site_icp":         row.text("WebIcp"),
			"company_mobile":   row.text("WebMsn"),
			"company_qq":       row.text("WebQQ"),
			"site_author":      row.text("Webauthor"),
			"site_copyright":   row.text("WebCopyright"),
			"company_name":     row.text("CoName"),
			"company_address":  row.text("CoAdd"),
			"company_postcode": row.text("CoPost"),
			"company_phone":    row.text("CoPhone"),
			"company_fax":      row.text("CoFax"),
			"company_contact":  row.text("CoRen"),
			"company_email":    row.text("CoEmail"),
		}
		for key, value := range values {
			if err := insertSettingIfMissing(ctx, transaction, key, value); err != nil {
				return err
			}
		}
	}
	shortIntro := ""
	for _, row := range labelRows {
		if strings.EqualFold(strings.TrimSpace(row.text("lname")), "#bm_about#") {
			shortIntro = row.text("lcontent")
			break
		}
	}
	legacyIntro := ""
	for _, row := range companyRows {
		if row.number("root") == 32 && row.text("coname") == "关于我们" {
			legacyIntro = row.text("Centern")
			break
		}
	}
	if shortIntro == "" {
		shortIntro = legacyIntro
	}
	if shortIntro != "" {
		if err := migrateCompanyIntro(ctx, transaction, shortIntro, legacyIntro); err != nil {
			return err
		}
	}
	for key, value := range customLabelSettings(labelRows) {
		if err := insertSettingIfMissing(ctx, transaction, key, value); err != nil {
			return err
		}
	}
	for _, row := range metaRows {
		id := row.number("id")
		if id < 1 {
			continue
		}
		prefix := "meta." + strconv.FormatInt(id, 10)
		values := map[string]string{
			prefix + ".name":        row.text("typename"),
			prefix + ".title":       row.text("title"),
			prefix + ".keywords":    row.text("meta_keywords"),
			prefix + ".description": row.text("meta_descriptions"),
		}
		if id == 1 {
			values["site_title"] = row.text("title")
			values["site_keywords"] = row.text("meta_keywords")
			values["site_description"] = row.text("meta_descriptions")
		}
		for key, value := range values {
			if err := insertSettingIfMissing(ctx, transaction, key, value); err != nil {
				return err
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit settings migration: %w", err)
	}
	return nil
}

func migrateCompanyIntro(ctx context.Context, transaction *sql.Tx, preferred, legacy string) error {
	var current string
	err := transaction.QueryRowContext(ctx, `SELECT "value" FROM "gocms_site_setting" WHERE "key" = ?`, "company_intro").Scan(&current)
	if err == sql.ErrNoRows {
		return insertSettingIfMissing(ctx, transaction, "company_intro", preferred)
	}
	if err != nil {
		return fmt.Errorf("read company intro setting: %w", err)
	}
	if strings.TrimSpace(current) == "" || (legacy != "" && current == legacy) {
		if _, err := transaction.ExecContext(ctx, `UPDATE "gocms_site_setting" SET "value" = ?, "updated_at" = CURRENT_TIMESTAMP WHERE "key" = ?`, preferred, "company_intro"); err != nil {
			return fmt.Errorf("update company intro setting: %w", err)
		}
	}
	return nil
}

func insertSettingIfMissing(ctx context.Context, transaction *sql.Tx, key, value string) error {
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO "gocms_site_setting" ("key", "value") VALUES (?, ?)
		ON CONFLICT ("key") DO NOTHING`, key, value); err != nil {
		return fmt.Errorf("migrate site setting %s: %w", key, err)
	}
	return nil
}
