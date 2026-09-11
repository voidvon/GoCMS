package legacyimport

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gocms/internal/templatelabel"
)

var (
	legacyCustomLabelPattern  = regexp.MustCompile(`(?i)#BM_[A-Za-z0-9_]+#`)
	legacyHopeMetaPattern     = regexp.MustCompile(`(?i)#HOPE_META_(TITLE|KEYWORDS|DESCRIPTION)\(([0-9]+)\)#`)
	legacyHopeVariablePattern = regexp.MustCompile(`(?i)#HOPE_[A-Za-z0-9_]+#`)
	legacyCategoriesPattern   = regexp.MustCompile(`(?i)#categories_plain\(\)#`)
)

type legacyTemplateLabel struct {
	key         string
	name        string
	categoryID  int64
	description string
	content     string
	sortOrder   int64
}

// importTemplateLabels converts the old reusable HTML fragments into the
// runtime label tables. The source rows are only available during migration;
// the publisher reads gocms_template_label exclusively.
func importTemplateLabels(ctx context.Context, transaction *sql.Tx, categoryRows, labelRows []sourceRow) error {
	categoryIDs, err := importLabelCategories(ctx, transaction, categoryRows)
	if err != nil {
		return err
	}
	existing, err := existingTemplateLabelKeys(ctx, transaction)
	if err != nil {
		return err
	}

	aliases := legacyLabelAliases(labelRows)
	baseCounts := make(map[string]int)
	items := make([]legacyTemplateLabel, 0, len(labelRows))
	for _, row := range sortedLegacyRows(labelRows, "id") {
		id := row.number("id")
		if id < 1 {
			continue
		}
		rawName := row.text("lname")
		if rawName == "" {
			continue
		}
		content := convertLegacyLabelContent(row.text("lcontent"), aliases)
		if strings.TrimSpace(content) == "" {
			continue
		}
		key := legacyLabelKey(rawName)
		baseCounts[key]++
		name := row.text("ldes")
		if name == "" {
			name = strings.Trim(strings.TrimSpace(rawName), "#")
		}
		if name == "" {
			name = key
		}
		description := row.text("ldes")
		if description == "" {
			description = "原系统标签：" + rawName
		} else {
			description += "；原系统标签：" + rawName
		}
		items = append(items, legacyTemplateLabel{
			key:         key,
			name:        name,
			categoryID:  categoryIDs[row.number("lkind")],
			description: description,
			content:     content,
			sortOrder:   id,
		})
	}

	for index, item := range items {
		key := item.key
		if baseCounts[item.key] > 1 {
			key = legacyLabelKeyWithSuffix(item.key, strconv.FormatInt(int64(index+1), 10))
		}
		// A key already present in the unified table is treated as migrated or
		// deliberately managed content and is left untouched on reruns.
		if existing[key] {
			continue
		}
		if err := templatelabel.ValidateContent(key, item.content); err != nil {
			return fmt.Errorf("validate migrated template label %s: %w", key, err)
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO "gocms_template_label"
			("key", "name", "category_id", "context", "description", "content", "sort_order")
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			key, item.name, item.categoryID, templatelabel.ContextAny, item.description, item.content, item.sortOrder); err != nil {
			return fmt.Errorf("import template label %s: %w", key, err)
		}
		existing[key] = true
	}
	return nil
}

func importLabelCategories(ctx context.Context, transaction *sql.Tx, rows []sourceRow) (map[int64]int64, error) {
	result := make(map[int64]int64, len(rows))
	for index, row := range sortedLegacyRows(rows, "id") {
		oldID := row.number("id")
		name := row.text("kindname")
		if oldID < 1 || name == "" {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO "gocms_template_label_category" ("name", "sort_order") VALUES (?, ?)
			ON CONFLICT ("name") DO NOTHING`, name, index); err != nil {
			return nil, fmt.Errorf("import template label category %s: %w", name, err)
		}
		var newID int64
		if err := transaction.QueryRowContext(ctx, `SELECT "id" FROM "gocms_template_label_category" WHERE "name" = ?`, name).Scan(&newID); err != nil {
			return nil, fmt.Errorf("read imported template label category %s: %w", name, err)
		}
		result[oldID] = newID
	}
	return result, nil
}

func existingTemplateLabelKeys(ctx context.Context, transaction *sql.Tx) (map[string]bool, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT "key" FROM "gocms_template_label"`)
	if err != nil {
		return nil, fmt.Errorf("read existing template label keys: %w", err)
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan existing template label key: %w", err)
		}
		result[strings.ToLower(strings.TrimSpace(key))] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read existing template label keys: %w", err)
	}
	return result, nil
}

func sortedLegacyRows(rows []sourceRow, idField string) []sourceRow {
	items := append([]sourceRow(nil), rows...)
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].number(idField) < items[right].number(idField)
	})
	return items
}

func legacyLabelAliases(rows []sourceRow) map[string]string {
	aliases := make(map[string]string, len(rows))
	for _, row := range sortedLegacyRows(rows, "id") {
		name := row.text("lname")
		if name == "" {
			continue
		}
		aliases[strings.ToLower(name)] = legacyLabelKey(name)
	}
	return aliases
}

func legacyLabelKey(raw string) string {
	raw = strings.ToLower(strings.Trim(strings.TrimSpace(raw), "#"))
	var builder strings.Builder
	separator := false
	for _, char := range raw {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			if separator && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(char)
			separator = false
			continue
		}
		if builder.Len() > 0 {
			separator = true
		}
	}
	key := strings.Trim(builder.String(), "-")
	if key == "" {
		return "label"
	}
	if key[0] >= '0' && key[0] <= '9' {
		key = "label-" + key
	}
	if len(key) > 64 {
		key = key[:64]
	}
	return key
}

func legacyLabelKeyWithSuffix(base, suffix string) string {
	ending := "-" + suffix
	if len(base)+len(ending) > 64 {
		base = base[:64-len(ending)]
	}
	return strings.TrimRight(base, "-") + ending
}

func convertLegacyLabelContent(content string, aliases map[string]string) string {
	content = legacyCategoriesPattern.ReplaceAllString(content, `{{range catalogCategories .}}<a href="{{.URL}}">{{.Name}}</a> | {{end}}`)
	content = legacyHopeMetaPattern.ReplaceAllStringFunc(content, func(value string) string {
		matches := legacyHopeMetaPattern.FindStringSubmatch(value)
		if len(matches) != 3 {
			return value
		}
		field := map[string]string{
			"title":       "title",
			"keywords":    "keywords",
			"description": "description",
		}[strings.ToLower(matches[1])]
		if field == "" {
			return value
		}
		return `{{setting "meta.` + matches[2] + `.` + field + `"}}`
	})
	content = legacyHopeVariablePattern.ReplaceAllStringFunc(content, func(value string) string {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(value, "#"), "#"))
		name = strings.TrimPrefix(name, "hope_")
		if setting, ok := legacyHopeSettings[name]; ok {
			return `{{setting "` + setting + `"}}`
		}
		if field, ok := legacyHopeFields[name]; ok {
			return `{{.` + field + `}}`
		}
		return value
	})
	return legacyCustomLabelPattern.ReplaceAllStringFunc(content, func(value string) string {
		if key, ok := aliases[strings.ToLower(value)]; ok {
			return `{{label "` + key + `" .}}`
		}
		return value
	})
}

var legacyHopeSettings = map[string]string{
	"webname":   "site_name",
	"weburl":    "site_url",
	"coname":    "company_name",
	"address":   "company_address",
	"post":      "company_postcode",
	"tel":       "company_phone",
	"fax":       "company_fax",
	"ren":       "company_contact",
	"email":     "company_email",
	"webicp":    "site_icp",
	"webqq":     "company_qq",
	"webmsn":    "company_mobile",
	"webauthor": "site_author",
	"copyright": "site_copyright",
}

var legacyHopeFields = map[string]string{
	"title":           "title",
	"catid":           "category_id",
	"catname":         "category_name",
	"body":            "body",
	"img":             "image",
	"prodcode":        "code",
	"proddescription": "description",
	"prodkeywords":    "keywords",
	"newskeywords":    "keywords",
	"newsdescription": "description",
	"co_centern":      "body",
}
