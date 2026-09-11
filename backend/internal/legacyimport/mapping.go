package legacyimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gocms/internal/sitehost"
)

type sourceRow map[string]string

func (row sourceRow) text(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(row[strings.ToLower(name)]); value != "" {
			return value
		}
	}
	return ""
}

func (row sourceRow) number(names ...string) int64 {
	value := row.text(names...)
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func flagValue(value int64) int64 {
	if value == 1 {
		return 1
	}
	return 0
}

type categorySource string

const (
	productCategories categorySource = "products"
	articleCategories categorySource = "articles"
	companyCategories categorySource = "company"
	jobCategoryName                  = "招聘"
)

type categoryMap map[int64]int64

func migrateRows(ctx context.Context, database *sql.DB) error {
	configRows, err := readRows(ctx, database, "benming_ch_config")
	if err != nil {
		return err
	}
	labelCategoryRows, err := readRows(ctx, database, "benming_ch_cuskind")
	if err != nil {
		return err
	}
	labelRows, err := readRows(ctx, database, "benming_ch_cuslabel")
	if err != nil {
		return err
	}
	companyRows, err := readRows(ctx, database, "benming_ch_Cocat")
	if err != nil {
		return err
	}
	contactRows, err := readRows(ctx, database, "benming_ch_Contact")
	if err != nil {
		return err
	}
	productCategoryRows, err := readRows(ctx, database, "benming_ch_ProdCat")
	if err != nil {
		return err
	}
	articleCategoryRows, err := readRows(ctx, database, "benming_ch_NewsCat")
	if err != nil {
		return err
	}
	productRows, err := readRows(ctx, database, "benming_ch_prod")
	if err != nil {
		return err
	}
	articleRows, err := readRows(ctx, database, "benming_ch_news")
	if err != nil {
		return err
	}
	jobRows, err := readRows(ctx, database, "benming_ch_job")
	if err != nil {
		return err
	}
	messageRows, err := readRows(ctx, database, "benming_ch_Msg")
	if err != nil {
		return err
	}
	metaRows, err := readRows(ctx, database, "benming_ch_MetaType")
	if err != nil {
		return err
	}
	adminRows, err := readRows(ctx, database, "benming_master")
	if err != nil {
		return err
	}

	publicHost := ""
	if len(configRows) > 0 {
		publicHost = sitehost.FromURL(configRows[0].text("WebUrl"))
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin data migration: %w", err)
	}
	defer transaction.Rollback()

	if err := importSettings(ctx, transaction, configRows); err != nil {
		return err
	}
	if err := importCustomLabelSettings(ctx, transaction, labelRows); err != nil {
		return err
	}
	if err := importTemplateLabels(ctx, transaction, labelCategoryRows, labelRows); err != nil {
		return err
	}
	if err := importMetaSettings(ctx, transaction, metaRows); err != nil {
		return err
	}

	companyMap, err := importCategories(ctx, transaction, companyCategories, companyRows, publicHost)
	if err != nil {
		return err
	}
	productMap, err := importCategories(ctx, transaction, productCategories, productCategoryRows, publicHost)
	if err != nil {
		return err
	}
	articleMap, err := importCategories(ctx, transaction, articleCategories, articleCategoryRows, publicHost)
	if err != nil {
		return err
	}

	contentMap := map[categorySource]map[int64]int64{
		productCategories: make(map[int64]int64),
		articleCategories: make(map[int64]int64),
		companyCategories: make(map[int64]int64),
		"jobs":            make(map[int64]int64),
	}
	if err := importCompanyContent(ctx, transaction, companyRows, companyMap, publicHost, contentMap[companyCategories]); err != nil {
		return err
	}
	if err := importProductContent(ctx, transaction, productRows, productMap, publicHost, contentMap[productCategories]); err != nil {
		return err
	}
	if err := importArticleContent(ctx, transaction, articleRows, articleMap, publicHost, contentMap[articleCategories]); err != nil {
		return err
	}
	if err := importJobContent(ctx, transaction, jobRows, publicHost, contentMap["jobs"]); err != nil {
		return err
	}

	aboutID, err := findAboutCategory(ctx, transaction, companyRows, companyMap)
	if err != nil {
		return err
	}
	if err := importContactCategory(ctx, transaction, aboutID, contactRows, configRows, publicHost); err != nil {
		return err
	}
	if err := importMessages(ctx, transaction, messageRows, contentMap, publicHost); err != nil {
		return err
	}
	if err := importAdmins(ctx, transaction, adminRows); err != nil {
		return err
	}
	if err := EnsureLegacyModelFields(ctx, database); err != nil {
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit data migration: %w", err)
	}
	return nil
}

func importSettings(ctx context.Context, transaction *sql.Tx, rows []sourceRow) error {
	if len(rows) == 0 {
		return nil
	}
	row := rows[0]
	values := map[string]string{
		"site_name":        row.text("WebName"),
		"site_url":         row.text("WebUrl"),
		"site_icp":         row.text("WebIcp"),
		"site_qq":          row.text("WebQQ"),
		"company_qq":       row.text("WebQQ"),
		"site_msn":         row.text("WebMsn"),
		"company_mobile":   row.text("WebMsn"),
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
		if err := upsertSetting(ctx, transaction, key, value); err != nil {
			return err
		}
	}
	return nil
}

func importCustomLabelSettings(ctx context.Context, transaction *sql.Tx, rows []sourceRow) error {
	for key, value := range customLabelSettings(rows) {
		if err := upsertSetting(ctx, transaction, key, value); err != nil {
			return err
		}
	}
	return nil
}

func importMetaSettings(ctx context.Context, transaction *sql.Tx, rows []sourceRow) error {
	for _, row := range rows {
		id := row.number("id")
		if id < 1 {
			continue
		}
		prefix := "meta." + strconv.FormatInt(id, 10)
		for key, value := range map[string]string{
			prefix + ".name":        row.text("typename"),
			prefix + ".title":       row.text("title"),
			prefix + ".keywords":    row.text("meta_keywords"),
			prefix + ".description": row.text("meta_descriptions"),
		} {
			if err := upsertSetting(ctx, transaction, key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertSetting(ctx context.Context, transaction *sql.Tx, key, value string) error {
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO "gocms_site_setting" ("key", "value") VALUES (?, ?)
		ON CONFLICT ("key") DO UPDATE SET "value" = excluded."value", "updated_at" = CURRENT_TIMESTAMP`, key, value); err != nil {
		return fmt.Errorf("import site setting %s: %w", key, err)
	}
	return nil
}

func importCategories(ctx context.Context, transaction *sql.Tx, source categorySource, rows []sourceRow, publicHost string) (categoryMap, error) {
	items := append([]sourceRow(nil), rows...)
	sort.SliceStable(items, func(left, right int) bool { return items[left].number("id") < items[right].number("id") })
	result := make(categoryMap, len(items))
	for _, row := range items {
		oldID := row.number("id")
		if oldID < 1 {
			continue
		}
		listPath, detailPath, pageSize := categoryRoutes(source, row, rows)
		detailTemplate := "content_detail.html"
		modelID := int64(1)
		if source == productCategories {
			detailTemplate = "product_detail.html"
			modelID = 2
		}
		resultID, err := transaction.ExecContext(ctx, `
			INSERT INTO "gocms_category"
			("name", "parent_id", "order_id", "list_page_size", "page_type", "route_id",
			 "list_path", "list_file_pattern", "list_template", "cover_template", "detail_path",
			 "detail_file_pattern", "detail_template", "keywords", "description", "cover_content", "model_id")
			VALUES (?, 0, ?, ?, 'list', ?, ?, '{id}.html', 'category_list.html', '', ?, '{id}.html', ?, ?, ?, '', ?)`,
			row.text("CatName", "coname"), row.number("Orderid", "ORderID", "orderid"), pageSize, oldID,
			listPath, detailPath, detailTemplate, row.text("key"), normalizeImageText(row.text("desc"), publicHost), modelID)
		if err != nil {
			return nil, fmt.Errorf("import %s category %d: %w", source, oldID, err)
		}
		newID, err := resultID.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("read imported %s category %d: %w", source, oldID, err)
		}
		result[oldID] = newID
	}
	for _, row := range items {
		oldID := row.number("id")
		newID := result[oldID]
		if newID == 0 {
			continue
		}
		parentID := result[row.number("Root", "root")]
		if _, err := transaction.ExecContext(ctx, `UPDATE "gocms_category" SET "parent_id" = ? WHERE "id" = ?`, parentID, newID); err != nil {
			return nil, fmt.Errorf("link imported %s category %d: %w", source, oldID, err)
		}
	}
	return result, nil
}

func categoryRoutes(source categorySource, row sourceRow, rows []sourceRow) (string, string, int64) {
	pageSize := int64(14)
	switch source {
	case productCategories:
		if row.number("Root") == 0 {
			return "valve", "Product", 12
		}
		return "Products", "Product", 12
	case articleCategories:
		rootID := row.number("id")
		parents := make(map[int64]int64, len(rows))
		for _, item := range rows {
			parents[item.number("id")] = item.number("Root")
		}
		seen := map[int64]bool{}
		for parents[rootID] > 0 && !seen[rootID] {
			seen[rootID] = true
			rootID = parents[rootID]
		}
		if rootID == 12 {
			return "service", "service/detail", 6
		}
		return "news", "news/detail", 6
	case companyCategories:
		return "about", "about/content", pageSize
	default:
		return "category", "content", pageSize
	}
}

func importCompanyContent(ctx context.Context, transaction *sql.Tx, rows []sourceRow, categories categoryMap, publicHost string, contentIDs map[int64]int64) error {
	for _, row := range rows {
		categoryID := categories[row.number("id")]
		if categoryID == 0 || strings.TrimSpace(row.text("coname")) == "" {
			continue
		}
		contentID, err := insertContent(ctx, transaction, contentInput{
			CategoryID: categoryID,
			RouteKey:   strconv.FormatInt(row.number("id"), 10),
			Title:      row.text("coname"),
			Body:       normalizeImageText(row.text("Centern"), publicHost),
			OrderID:    row.number("orderid"),
			Visible:    1,
		})
		if err != nil {
			return fmt.Errorf("import company content %d: %w", row.number("id"), err)
		}
		contentIDs[row.number("id")] = contentID
	}
	return nil
}

func importProductContent(ctx context.Context, transaction *sql.Tx, rows []sourceRow, categories categoryMap, publicHost string, contentIDs map[int64]int64) error {
	for _, row := range rows {
		image := row.text("bigpic")
		if image == "" || strings.Contains(strings.ToLower(image), "dfpic.gif") {
			image = row.text("smallpic")
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(image)), "/skin/dfpic.gif") || strings.EqualFold(strings.TrimSpace(image), "skin/dfpic.gif") {
			image = ""
		}
		code := row.text("prodCode")
		summary := normalizeImageText(row.text("remark"), publicHost)
		extra := extractLegacyProductParameters(code, summary)
		extraData := "{}"
		if len(extra) > 0 {
			if b, err := json.Marshal(extra); err == nil {
				extraData = string(b)
			}
		}

		contentID, err := insertContent(ctx, transaction, contentInput{
			CategoryID: categories[row.number("CatId")],
			RouteKey:   strconv.FormatInt(row.number("id"), 10),
			Title:      row.text("prodName"),
			Code:       code,
			Summary:    summary,
			Body:       normalizeImageText(row.text("itemize"), publicHost),
			Image:      normalizeImageText(image, publicHost),
			Keywords:   row.text("key"),
			OrderID:    row.number("orderid"),
			Featured:   flagValue(row.number("tjhome")),
			Visible:    flagValue(row.number("show")),
			ModelID:    2,
			ExtraData:  extraData,
		})
		if err != nil {
			return fmt.Errorf("import product content %d: %w", row.number("id"), err)
		}
		contentIDs[row.number("id")] = contentID
	}
	return nil
}

func importArticleContent(ctx context.Context, transaction *sql.Tx, rows []sourceRow, categories categoryMap, publicHost string, contentIDs map[int64]int64) error {
	for _, row := range rows {
		contentID, err := insertContent(ctx, transaction, contentInput{
			CategoryID:  categories[row.number("Typeid")],
			RouteKey:    strconv.FormatInt(row.number("newsid"), 10),
			Title:       row.text("Title"),
			Summary:     normalizeImageText(row.text("desc"), publicHost),
			Body:        normalizeImageText(row.text("Content"), publicHost),
			Image:       normalizeImageText(row.text("Picture"), publicHost),
			Published:   row.text("Dateandtime"),
			Source:      row.text("Nfrom"),
			Keywords:    row.text("key"),
			Description: normalizeImageText(row.text("desc"), publicHost),
			OrderID:     row.number("newsid"),
			Featured:    flagValue(row.number("tjhome")),
			Visible:     1,
			ModelID:     1,
		})
		if err != nil {
			return fmt.Errorf("import article content %d: %w", row.number("newsid"), err)
		}
		contentIDs[row.number("newsid")] = contentID
	}
	return nil
}

func importJobContent(ctx context.Context, transaction *sql.Tx, rows []sourceRow, publicHost string, contentIDs map[int64]int64) error {
	if len(rows) == 0 {
		return nil
	}
	categoryID, err := insertRuntimeCategory(ctx, transaction, jobCategoryName, "jobs", "jobs/content", 10)
	if err != nil {
		return err
	}
	for _, row := range rows {
		body := normalizeImageText(row.text("jobneed"), publicHost)
		if address := row.text("address"); address != "" {
			body = `<p><strong>工作地点：</strong>` + html.EscapeString(address) + `</p>` + body
		}
		contentID, err := insertContent(ctx, transaction, contentInput{
			CategoryID: categoryID,
			RouteKey:   strconv.FormatInt(row.number("id"), 10),
			Title:      row.text("jobName"),
			Summary:    row.text("address"),
			Body:       body,
			Published:  row.text("date"),
			Source:     row.text("linkren"),
			Code:       row.text("phone"),
			Visible:    flagValue(row.number("state")),
			ModelID:    1,
		})
		if err != nil {
			return fmt.Errorf("import job content %d: %w", row.number("id"), err)
		}
		contentIDs[row.number("id")] = contentID
	}
	return nil
}

type contentInput struct {
	CategoryID  int64
	RouteKey    string
	Title       string
	Code        string
	Summary     string
	Body        string
	Image       string
	Published   string
	Source      string
	Keywords    string
	Description string
	OrderID     int64
	Featured    int64
	Visible     int64
	ModelID     int64
	ExtraData   string
}

func insertContent(ctx context.Context, transaction *sql.Tx, item contentInput) (int64, error) {
	modelID := item.ModelID
	if modelID <= 0 {
		modelID = 1
	}
	extraData := item.ExtraData
	if extraData == "" {
		extraData = "{}"
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO "gocms_content"
		("category_id", "route_key", "title", "code", "summary", "body", "cover_image", "published_at", "source", "keywords", "description", "sort_order", "featured", "visible", "model_id", "extra_data")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.CategoryID, item.RouteKey, item.Title, item.Code, item.Summary, item.Body, item.Image, item.Published,
		item.Source, item.Keywords, item.Description, item.OrderID, item.Featured, item.Visible, modelID, extraData)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func insertRuntimeCategory(ctx context.Context, transaction *sql.Tx, name, listPath, detailPath string, pageSize int64) (int64, error) {
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO "gocms_category"
		("name", "parent_id", "order_id", "list_page_size", "page_type", "route_id", "list_path", "list_file_pattern", "list_template", "cover_template", "detail_path", "detail_file_pattern", "detail_template")
		VALUES (?, 0, 0, ?, 'list', 0, ?, '{id}.html', 'category_list.html', '', ?, '{id}.html', 'content_detail.html')`,
		name, pageSize, listPath, detailPath)
	if err != nil {
		return 0, fmt.Errorf("create imported category %s: %w", name, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read imported category %s: %w", name, err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE "gocms_category" SET "route_id" = ? WHERE "id" = ?`, id, id); err != nil {
		return 0, fmt.Errorf("initialize imported category %s: %w", name, err)
	}
	return id, nil
}

func findAboutCategory(ctx context.Context, transaction *sql.Tx, rows []sourceRow, categories categoryMap) (int64, error) {
	for _, row := range rows {
		if strings.Contains(row.text("coname"), "关于") {
			if id := categories[row.number("id")]; id != 0 {
				return id, nil
			}
		}
	}
	return insertRuntimeCategory(ctx, transaction, "关于我们", "about", "about/content", 14)
}

func importContactCategory(ctx context.Context, transaction *sql.Tx, parentID int64, contactRows, configRows []sourceRow, publicHost string) error {
	contact := sourceRow{}
	if len(contactRows) > 0 {
		contact = contactRows[0]
	}
	config := sourceRow{}
	if len(configRows) > 0 {
		config = configRows[0]
	}
	body := contactBody(contact, config, publicHost)
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO "gocms_category"
		("name", "parent_id", "order_id", "list_page_size", "page_type", "route_id", "list_path", "list_file_pattern", "list_template", "cover_template", "detail_path", "detail_file_pattern", "detail_template", "cover_content")
		VALUES ('联系我们', ?, 0, 14, 'cover', 0, '', 'contact.html', 'category_list.html', 'category_cover.html', 'content', '{id}.html', 'content_detail.html', ?)`, parentID, body)
	if err != nil {
		return fmt.Errorf("import contact category: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read contact category: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE "gocms_category" SET "route_id" = ? WHERE "id" = ?`, id, id); err != nil {
		return fmt.Errorf("initialize contact category: %w", err)
	}
	return nil
}

func contactBody(contact, config sourceRow, publicHost string) string {
	value := func(contactKey, configKey string) string {
		if result := contact.text(contactKey); result != "" {
			return result
		}
		return config.text(configKey)
	}
	fields := [][2]string{
		{"地址", value("address", "CoAdd")},
		{"电话", value("phone", "CoPhone")},
		{"传真", value("fax", "CoFax")},
		{"联系人", value("linkren", "CoRen")},
		{"邮箱", value("Email", "CoEmail")},
		{"邮编", value("Post", "CoPost")},
	}
	var builder strings.Builder
	builder.WriteString(`<dl class="contact-details">`)
	for _, field := range fields {
		if strings.TrimSpace(field[1]) == "" {
			continue
		}
		builder.WriteString(`<dt>` + html.EscapeString(field[0]) + `</dt><dd>` + html.EscapeString(normalizeImageText(field[1], publicHost)) + `</dd>`)
	}
	builder.WriteString(`</dl>`)
	return builder.String()
}

func importMessages(ctx context.Context, transaction *sql.Tx, rows []sourceRow, contentIDs map[categorySource]map[int64]int64, publicHost string) error {
	for _, row := range rows {
		contentID := contentIDs[productCategories][row.number("prodid")]
		if contentID == 0 {
			contentID = contentIDs[articleCategories][row.number("prodid")]
		}
		if contentID == 0 {
			contentID = contentIDs["jobs"][row.number("prodid")]
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO "gocms_message"
			("id", "title", "name", "phone", "mobile", "fax", "email", "address", "content", "created_at", "state", "content_id")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.number("id"), row.text("Title"), row.text("linkren"), row.text("phone"), row.text("mobile"), row.text("fax"),
			row.text("email"), row.text("address"), normalizeImageText(row.text("content"), publicHost), row.text("date"),
			flagValue(row.number("state")), contentID); err != nil {
			return fmt.Errorf("import message %d: %w", row.number("id"), err)
		}
	}
	return nil
}

func importAdmins(ctx context.Context, transaction *sql.Tx, rows []sourceRow) error {
	for _, row := range rows {
		username := row.text("UserName")
		if username == "" {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO "gocms_admin_user" ("id", "username", "password_hash", "flags", "last_login", "last_login_ip")
			VALUES (?, ?, '', ?, NULLIF(?, ''), NULLIF(?, ''))`,
			row.number("Id"), username, row.text("Flag"), row.text("LastLogin"), row.text("LastLoginIp")); err != nil {
			return fmt.Errorf("import administrator %s: %w", username, err)
		}
	}
	return nil
}

var imageToken = regexp.MustCompile(`(?i)(?:https?://[^"'\s<>\)]+|//[^"'\s<>\)]+|(?:[a-z]:)?[\\/]*(?:uploadfile|produppic)[^"'\s<>\)]+)`)

func normalizeImageText(value, publicHost string) string {
	return imageToken.ReplaceAllStringFunc(value, func(token string) string {
		normalized := strings.ReplaceAll(token, `\`, "/")
		parsed, err := url.Parse(normalized)
		if err != nil || parsed.Path == "" {
			return token
		}
		if parsed.Host != "" && !sitehost.Matches(parsed.Hostname(), publicHost) {
			return token
		}
		trimmed := strings.TrimPrefix(parsed.Path, "/")
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "uploadfile/") && !strings.HasPrefix(lower, "produppic/") {
			return token
		}
		filename := path.Base(trimmed)
		if filename == "." || filename == "/" || filename == "" {
			return token
		}
		parsed.Scheme, parsed.Host, parsed.Opaque = "", "", ""
		parsed.User = nil
		parsed.RawPath = ""
		parsed.Path = "/images/" + filename
		return parsed.String()
	})
}

var (
	reLegacySpec        = regexp.MustCompile(`(?:型号|规格)[：:\s]+([^，,；;\s\n\r]+)`)
	reLegacyCaliber     = regexp.MustCompile(`(?:口径|通径)[：:\s]+([^，,；;\s\n\r]+)`)
	reLegacyMaterial    = regexp.MustCompile(`(?:材质|阀体材质)[：:\s]+([^，,；;\s\n\r]+)`)
	reLegacyPressure    = regexp.MustCompile(`(?:压力|公称压力)[：:\s]+([^，,；;\s\n\r]+)`)
	reLegacyTemperature = regexp.MustCompile(`(?:温度|适用温度|工作温度)[：:\s]+([^，,；;\s\n\r]+)`)
	reLegacyMedium      = regexp.MustCompile(`(?:介质|适用介质)[：:\s]+([^，,；;\s\n\r]+)`)
)

func cleanLegacyParam(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "，,。;；:：")
	return s
}

func extractLegacyProductParameters(code, summary string) map[string]string {
	extra := make(map[string]string)
	if m := reLegacySpec.FindStringSubmatch(summary); len(m) > 1 {
		extra["spec"] = cleanLegacyParam(m[1])
	} else if strings.TrimSpace(code) != "" {
		extra["spec"] = strings.TrimSpace(code)
	}

	if m := reLegacyCaliber.FindStringSubmatch(summary); len(m) > 1 {
		extra["caliber"] = cleanLegacyParam(m[1])
	}
	if m := reLegacyMaterial.FindStringSubmatch(summary); len(m) > 1 {
		extra["material"] = cleanLegacyParam(m[1])
	}
	if m := reLegacyPressure.FindStringSubmatch(summary); len(m) > 1 {
		extra["pressure"] = cleanLegacyParam(m[1])
	}
	if m := reLegacyTemperature.FindStringSubmatch(summary); len(m) > 1 {
		extra["temperature"] = cleanLegacyParam(m[1])
	}
	if m := reLegacyMedium.FindStringSubmatch(summary); len(m) > 1 {
		extra["medium"] = cleanLegacyParam(m[1])
	}
	return extra
}

func EnsureLegacyModelFields(ctx context.Context, database *sql.DB) error {
	var tableID int64
	err := database.QueryRowContext(ctx, `SELECT "id" FROM "gocms_model_table" WHERE "table_name" = 'product'`).Scan(&tableID)
	if err != nil {
		return nil
	}

	fields := []struct {
		name      string
		label     string
		fieldType string
		options   string
		desc      string
		order     int
	}{
		{"spec", "规格型号", "text", "", "如 ANSI 150LB~600LB, Z41H系列等", 100},
		{"material", "阀体材质", "select", "铸钢==铸钢\n不锈钢==不锈钢\n球墨铸铁==球墨铸铁\n铸铁==铸铁\n黄铜==黄铜\n合金钢==合金钢\n锻钢==锻钢\nPVC/塑料==PVC/塑料", "阀门阀体及关键部件材质", 110},
		{"pressure", "公称压力", "select", "PN1.6MPa==PN1.6MPa\nPN2.5MPa==PN2.5MPa\nPN4.0MPa==PN4.0MPa\nPN6.4MPa==PN6.4MPa\nPN10.0MPa==PN10.0MPa\n150LB==150LB\n300LB==300LB\n600LB==600LB\n10K==10K\n20K==20K", "公称工作压力等级", 120},
		{"caliber", "公称通径", "text", "", "如 DN15~DN600, 1/2\"~24\"", 130},
		{"temperature", "适用温度", "text", "", "如 -20℃~425℃", 140},
		{"medium", "适用介质", "text", "", "如 水、蒸汽、油品、气体、腐蚀性介质等", 150},
	}

	for _, f := range fields {
		_, _ = database.ExecContext(ctx, `
			INSERT OR IGNORE INTO "gocms_model_field" ("table_id", "field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system")
			VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
			tableID, f.name, f.label, f.fieldType, f.options, f.desc, f.order)
	}

	var modelID int64
	if err := database.QueryRowContext(ctx, `SELECT "id" FROM "gocms_model" WHERE "table_id" = ? AND "name" = '产品系统模型'`, tableID).Scan(&modelID); err == nil {
		var existingEntryFields string
		if err := database.QueryRowContext(ctx, `SELECT "entry_fields" FROM "gocms_model" WHERE "id" = ?`, modelID).Scan(&existingEntryFields); err == nil {
			var items []map[string]string
			_ = json.Unmarshal([]byte(existingEntryFields), &items)
			hasField := func(f string) bool {
				for _, item := range items {
					if item["field"] == f {
						return true
					}
				}
				return false
			}
			added := false
			for _, f := range fields {
				if !hasField(f.name) {
					items = append(items, map[string]string{"field": f.name, "label": f.label})
					added = true
				}
			}
			if added {
				updatedJSON, _ := json.Marshal(items)
				_, _ = database.ExecContext(ctx, `UPDATE "gocms_model" SET "entry_fields" = ? WHERE "id" = ?`, string(updatedJSON), modelID)
			}
		}
	}
	return nil
}
