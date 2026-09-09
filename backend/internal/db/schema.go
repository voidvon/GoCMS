package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"bilvie/internal/routing"
	"bilvie/internal/templateconfig"
)

type ColumnType int

const (
	ColumnText ColumnType = iota
	ColumnInteger
	ColumnDateTime
)

type Column struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
}

type Table struct {
	Name    string
	Columns []Column
}

// AccessTables mirrors the legacy Access schema. Keeping the original names
// makes the first migration easy to compare with the ASP code.
var AccessTables = []Table{
	{
		Name: "benming_ch_Cocat",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true},
			{Name: "coname", Type: ColumnText}, {Name: "root", Type: ColumnInteger},
			{Name: "orderid", Type: ColumnInteger}, {Name: "sitepath", Type: ColumnInteger},
			{Name: "siteurl", Type: ColumnText}, {Name: "Centern", Type: ColumnText},
		},
	},
	{
		Name: "benming_ch_config",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true},
			{Name: "WebName", Type: ColumnText}, {Name: "WebUrl", Type: ColumnText},
			{Name: "WebIcp", Type: ColumnText}, {Name: "WebQQ", Type: ColumnText},
			{Name: "WebMsn", Type: ColumnText}, {Name: "Webauthor", Type: ColumnText},
			{Name: "WebCopyright", Type: ColumnText}, {Name: "CoName", Type: ColumnText},
			{Name: "CoAdd", Type: ColumnText}, {Name: "CoPost", Type: ColumnText},
			{Name: "CoPhone", Type: ColumnText}, {Name: "CoFax", Type: ColumnText},
			{Name: "CoRen", Type: ColumnText}, {Name: "CoEmail", Type: ColumnText},
			{Name: "benming", Type: ColumnText},
		},
	},
	{
		Name: "benming_ch_Contact",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true},
			{Name: "offname", Type: ColumnText}, {Name: "address", Type: ColumnText},
			{Name: "phone", Type: ColumnText}, {Name: "fax", Type: ColumnText},
			{Name: "linkren", Type: ColumnText}, {Name: "Email", Type: ColumnText},
			{Name: "Post", Type: ColumnText},
		},
	},
	{
		Name:    "benming_ch_cuskind",
		Columns: []Column{{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "kindname", Type: ColumnText}},
	},
	{
		Name: "benming_ch_cuslabel",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "lname", Type: ColumnText},
			{Name: "ldes", Type: ColumnText}, {Name: "lcontent", Type: ColumnText},
			{Name: "lkind", Type: ColumnInteger}, {Name: "lidate", Type: ColumnDateTime},
		},
	},
	{
		Name: "benming_ch_job",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "jobName", Type: ColumnText},
			{Name: "address", Type: ColumnText}, {Name: "jobnob", Type: ColumnInteger},
			{Name: "jobneed", Type: ColumnText}, {Name: "linkren", Type: ColumnText},
			{Name: "phone", Type: ColumnText}, {Name: "state", Type: ColumnInteger},
			{Name: "date", Type: ColumnDateTime},
		},
	},
	{
		Name: "benming_ch_MetaType",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "typename", Type: ColumnText},
			{Name: "meta_keywords", Type: ColumnText}, {Name: "meta_descriptions", Type: ColumnText},
			{Name: "title", Type: ColumnText},
		},
	},
	{
		Name: "benming_ch_Msg",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "Title", Type: ColumnText},
			{Name: "linkren", Type: ColumnText}, {Name: "phone", Type: ColumnText},
			{Name: "mobile", Type: ColumnText}, {Name: "fax", Type: ColumnText},
			{Name: "email", Type: ColumnText}, {Name: "content", Type: ColumnText},
			{Name: "date", Type: ColumnDateTime}, {Name: "address", Type: ColumnText},
			{Name: "state", Type: ColumnInteger}, {Name: "statedate", Type: ColumnDateTime},
			{Name: "prodid", Type: ColumnInteger},
		},
	},
	{
		Name: "benming_ch_news",
		Columns: []Column{
			{Name: "newsid", Type: ColumnInteger, PrimaryKey: true}, {Name: "Title", Type: ColumnText},
			{Name: "Content", Type: ColumnText}, {Name: "Typeid", Type: ColumnInteger},
			{Name: "Tjnews", Type: ColumnInteger}, {Name: "Nfrom", Type: ColumnText},
			{Name: "Picture", Type: ColumnText}, {Name: "Dateandtime", Type: ColumnDateTime},
			{Name: "hits", Type: ColumnInteger}, {Name: "tjhome", Type: ColumnInteger},
			{Name: "homepic", Type: ColumnInteger}, {Name: "html_pass", Type: ColumnInteger},
			{Name: "homehot", Type: ColumnInteger}, {Name: "upsize_ts", Type: ColumnText},
			{Name: "pictext", Type: ColumnInteger}, {Name: "key", Type: ColumnText},
			{Name: "desc", Type: ColumnText},
		},
	},
	{
		Name: "benming_ch_NewsCat",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "CatName", Type: ColumnText},
			{Name: "Root", Type: ColumnInteger}, {Name: "ORderID", Type: ColumnInteger},
		},
	},
	{
		Name: "benming_ch_prod",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "prodName", Type: ColumnText},
			{Name: "prodCode", Type: ColumnText}, {Name: "CatId", Type: ColumnInteger},
			{Name: "remark", Type: ColumnText}, {Name: "itemize", Type: ColumnText},
			{Name: "smallpic", Type: ColumnText}, {Name: "bigpic", Type: ColumnText},
			{Name: "key", Type: ColumnText}, {Name: "orderid", Type: ColumnInteger},
			{Name: "tjhome", Type: ColumnInteger}, {Name: "show", Type: ColumnInteger},
		},
	},
	{
		Name: "benming_ch_ProdCat",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "CatName", Type: ColumnText},
			{Name: "Root", Type: ColumnInteger}, {Name: "Orderid", Type: ColumnInteger},
			{Name: "key", Type: ColumnText}, {Name: "desc", Type: ColumnText},
			{Name: "pic", Type: ColumnText}, {Name: "over_pic", Type: ColumnText},
		},
	},
	{
		Name: "benming_ch_prodphoto",
		Columns: []Column{
			{Name: "id", Type: ColumnInteger, PrimaryKey: true}, {Name: "photoName", Type: ColumnText},
			{Name: "photopic", Type: ColumnText}, {Name: "date", Type: ColumnDateTime},
		},
	},
	{
		Name: "benming_ch_worldec_Temp",
		Columns: []Column{
			{Name: "Id", Type: ColumnInteger, PrimaryKey: true}, {Name: "home_index", Type: ColumnText},
			{Name: "Co_index", Type: ColumnText}, {Name: "produts_index", Type: ColumnText},
			{Name: "produts_sort1", Type: ColumnText}, {Name: "produts_sort2", Type: ColumnText},
			{Name: "produts_sort3", Type: ColumnText}, {Name: "produts_detail", Type: ColumnText},
			{Name: "job_index", Type: ColumnText}, {Name: "Job_sort", Type: ColumnText},
			{Name: "Job_detail", Type: ColumnText}, {Name: "news_index", Type: ColumnText},
			{Name: "News_sort1", Type: ColumnText}, {Name: "news_detail", Type: ColumnText},
			{Name: "service_index", Type: ColumnText}, {Name: "service_sort1", Type: ColumnText},
			{Name: "service_detail", Type: ColumnText}, {Name: "msg_index", Type: ColumnText},
			{Name: "Contact", Type: ColumnText}, {Name: "Shou_index", Type: ColumnText},
			{Name: "tempname", Type: ColumnText}, {Name: "selected", Type: ColumnInteger},
		},
	},
	{
		Name: "benming_master",
		Columns: []Column{
			{Name: "Id", Type: ColumnInteger, PrimaryKey: true}, {Name: "UserName", Type: ColumnText},
			{Name: "PassWord", Type: ColumnText}, {Name: "Flag", Type: ColumnText},
			{Name: "LastLogin", Type: ColumnDateTime}, {Name: "LastLoginIp", Type: ColumnText},
		},
	},
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func columnSQLType(column Column) string {
	if column.Type == ColumnInteger {
		return "INTEGER"
	}
	return "TEXT"
}

func (table Table) createSQL() string {
	definitions := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		definition := fmt.Sprintf("%s %s", quoteIdentifier(column.Name), columnSQLType(column))
		if column.PrimaryKey {
			definition += " PRIMARY KEY"
		}
		definitions = append(definitions, definition)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", quoteIdentifier(table.Name), strings.Join(definitions, ", "))
}

func CreateSchema(ctx context.Context, database *sql.DB) error {
	for _, table := range AccessTables {
		if _, err := database.ExecContext(ctx, table.createSQL()); err != nil {
			return fmt.Errorf("create table %s: %w", table.Name, err)
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_prod_search ON "benming_ch_prod" ("show", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_prod_category ON "benming_ch_prod" ("CatId", "orderid", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_prod_name ON "benming_ch_prod" ("prodName")`,
		`CREATE INDEX IF NOT EXISTS idx_news_category ON "benming_ch_news" ("Typeid", "newsid")`,
		`CREATE INDEX IF NOT EXISTS idx_newscat_root ON "benming_ch_NewsCat" ("Root", "ORderID")`,
		`CREATE INDEX IF NOT EXISTS idx_prodcat_root ON "benming_ch_ProdCat" ("Root", "Orderid")`,
		`CREATE INDEX IF NOT EXISTS idx_msg_date ON "benming_ch_Msg" ("date")`,
	}
	for _, statement := range indexes {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	if err := EnsureLegacyCategoryRouteColumns(ctx, database); err != nil {
		return err
	}
	if err := EnsureUnifiedCategories(ctx, database); err != nil {
		return err
	}
	if err := EnsureContent(ctx, database); err != nil {
		return err
	}
	if err := EnsureMessages(ctx, database); err != nil {
		return err
	}
	return templateconfig.Ensure(ctx, database)
}

// EnsureLegacyCategoryRouteColumns adds route settings to the imported source
// table. This is used only while importing the old Access database; the
// publisher reads bilvie_category exclusively.
func EnsureLegacyCategoryRouteColumns(ctx context.Context, database *sql.DB) error {
	columns := map[string]string{
		"ListPath":          "TEXT",
		"ListFilePattern":   "TEXT",
		"ListTemplate":      "TEXT",
		"DetailPath":        "TEXT",
		"DetailFilePattern": "TEXT",
		"DetailTemplate":    "TEXT",
	}
	for name, columnType := range columns {
		var exists int
		if err := database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('benming_ch_ProdCat') WHERE name = ?`, name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect legacy category route column %s: %w", name, err)
		}
		if exists > 0 {
			continue
		}
		if _, err := database.ExecContext(ctx, `ALTER TABLE "benming_ch_ProdCat" ADD COLUMN "`+name+`" `+columnType); err != nil {
			return fmt.Errorf("add legacy category route column %s: %w", name, err)
		}
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "benming_ch_ProdCat"
		SET "ListPath" = CASE WHEN COALESCE("Root", 0) = 0 THEN ? ELSE ? END
		WHERE TRIM(COALESCE("ListPath", '')) = ''`, legacyRootListPath, legacyChildListPath); err != nil {
		return fmt.Errorf("initialize legacy category list paths: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "benming_ch_ProdCat"
		SET "ListFilePattern" = ?
		WHERE TRIM(COALESCE("ListFilePattern", '')) = ''`, routing.DefaultListPattern); err != nil {
		return fmt.Errorf("initialize legacy category list file patterns: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "benming_ch_ProdCat"
		SET "ListTemplate" = ?
		WHERE TRIM(COALESCE("ListTemplate", '')) = ''`, templateconfig.DefaultListTemplate); err != nil {
		return fmt.Errorf("initialize legacy category list templates: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "benming_ch_ProdCat"
		SET "DetailPath" = ?
		WHERE TRIM(COALESCE("DetailPath", '')) = ''`, legacyDetailPath); err != nil {
		return fmt.Errorf("initialize legacy category detail paths: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "benming_ch_ProdCat"
		SET "DetailFilePattern" = ?
		WHERE TRIM(COALESCE("DetailFilePattern", '')) = ''`, routing.DefaultDetailPattern); err != nil {
		return fmt.Errorf("initialize legacy category detail file patterns: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "benming_ch_ProdCat"
		SET "DetailTemplate" = ?
		WHERE TRIM(COALESCE("DetailTemplate", '')) = ''`, templateconfig.DefaultDetailTemplate); err != nil {
		return fmt.Errorf("initialize legacy category detail templates: %w", err)
	}
	return nil
}

func EnsureTemplateAssignments(ctx context.Context, database *sql.DB) error {
	return templateconfig.Ensure(ctx, database)
}
