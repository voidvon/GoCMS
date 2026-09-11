package legacyimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gocms/internal/templatelabel"
)

// MigrateExistingSettings repairs a database produced by an earlier import.
// It is a one-time migration step; the runtime publisher never reads the
// source tables after these values are copied into the unified settings table.
func MigrateExistingSettings(ctx context.Context, database *sql.DB) error {
	configRows, err := readRows(ctx, database, "benming_ch_config")
	if err != nil {
		return err
	}
	labelCategoryRows, err := readRows(ctx, database, "benming_ch_cuskind")
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
	if err := templatelabel.Ensure(ctx, database); err != nil {
		return fmt.Errorf("ensure template labels: %w", err)
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
	if err := importTemplateLabels(ctx, transaction, labelCategoryRows, labelRows); err != nil {
		return err
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
	if err := migrateLegacyCategoriesAndProducts(ctx, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit settings migration: %w", err)
	}
	_ = EnsureLegacyModelFields(ctx, database)
	return nil
}

func migrateLegacyCategoriesAndProducts(ctx context.Context, transaction *sql.Tx) error {
	var productModelID int64
	_ = transaction.QueryRowContext(ctx, `SELECT "id" FROM "gocms_model" WHERE "name" = '产品系统模型'`).Scan(&productModelID)
	if productModelID <= 0 {
		return nil
	}

	_, _ = transaction.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "model_id" = ?
		WHERE "list_path" IN ('valve', 'Products') AND "model_id" != ?`,
		productModelID, productModelID)

	_, _ = transaction.ExecContext(ctx, `
		UPDATE "gocms_content"
		SET "model_id" = ?
		WHERE "category_id" IN (SELECT "id" FROM "gocms_category" WHERE "model_id" = ?)
		  AND "model_id" != ?`,
		productModelID, productModelID, productModelID)

	rows, err := transaction.QueryContext(ctx, `
		SELECT "id", "code", "summary"
		FROM "gocms_content"
		WHERE "model_id" = ? AND ("extra_data" = '' OR "extra_data" = '{}' OR "extra_data" IS NULL)`,
		productModelID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type itemUpdate struct {
		id   int64
		data string
	}
	var updates []itemUpdate
	for rows.Next() {
		var id int64
		var code, summary string
		if err := rows.Scan(&id, &code, &summary); err == nil {
			extra := extractLegacyProductParameters(code, summary)
			if len(extra) > 0 {
				if b, err := json.Marshal(extra); err == nil {
					updates = append(updates, itemUpdate{id: id, data: string(b)})
				}
			}
		}
	}
	_ = rows.Close()

	for _, u := range updates {
		_, _ = transaction.ExecContext(ctx, `UPDATE "gocms_content" SET "extra_data" = ? WHERE "id" = ?`, u.data, u.id)
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
