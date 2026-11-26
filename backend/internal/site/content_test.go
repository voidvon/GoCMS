package site

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSaveAndPublishSetsContentDate(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	category := createContentTestCategory(t, server, database, token, "发布栏目", 0)
	const collection = "/api/admin/content"
	const publish = "?publish=1&publish_utc_offset=345"
	location := time.FixedZone("", 345*60)
	save := func(method, path, title string, visible int, date string) {
		t.Helper()
		payload := fmt.Sprintf(`{"title":%q,"category_id":%d,"visible":%d,"published_at":%q}`, title, category, visible, date)
		response := categoryRequest(t, server, token, method, path, payload)
		if response.Code != http.StatusOK {
			t.Fatalf("save returned %d: %s", response.Code, response.Body.String())
		}
	}
	listItem := func(title string) AdminContentItem {
		t.Helper()
		response := categoryRequest(t, server, token, http.MethodGet, collection+"?q="+url.QueryEscape(title), "")
		var page AdminContentPage
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
			t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
		}
		return page.Items[0]
	}
	checkCurrentDate := func(date string, before time.Time) {
		t.Helper()
		published, err := time.ParseInLocation(time.DateTime, date, location)
		if err != nil || published.Before(before) || published.After(time.Now()) {
			t.Fatalf("publication date %q is outside the request time in the editor's time zone: %v", date, err)
		}
	}

	before := time.Now().Truncate(time.Second)
	save(http.MethodPost, collection+publish, "新建内容", 1, " ")
	checkCurrentDate(listItem("新建内容").PublishedAt, before)

	save(http.MethodPost, collection, "草稿内容", 0, "")
	draft := listItem("草稿内容")
	if draft.PublishedAt != "" {
		t.Fatalf("only saving a draft set the publication date to %q", draft.PublishedAt)
	}
	itemPath := fmt.Sprintf("%s/%d", collection, draft.ID)
	before = time.Now().Truncate(time.Second)
	save(http.MethodPut, itemPath+publish, draft.Title, 1, "")
	checkCurrentDate(listItem(draft.Title).PublishedAt, before)

	save(http.MethodPut, itemPath+publish, draft.Title, 1, "2024-03-02T12:34")
	const manual = "2024-03-02 12:34:00"
	if got := listItem(draft.Title).PublishedAt; got != manual {
		t.Fatalf("manual publication date = %q, want %q", got, manual)
	}
	save(http.MethodPut, itemPath+publish, draft.Title, 1, "")
	if got := listItem(draft.Title).PublishedAt; got != manual {
		t.Fatalf("republishing replaced the saved date with %q, want %q", got, manual)
	}
}

func TestAdminContentUpdateTime(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	const collection = "/api/admin/content"
	const publishedAt = "2024-03-02 12:34:00"
	if _, err := database.Exec(`INSERT INTO gocms_content (id, title, published_at) VALUES (1, '历史内容', ?)`, publishedAt); err != nil {
		t.Fatal(err)
	}
	list := func() AdminContentPage {
		t.Helper()
		response := categoryRequest(t, server, token, http.MethodGet, collection, "")
		var page AdminContentPage
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Items) == 0 {
			t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
		}
		return page
	}
	checkTime := func(value string, before time.Time) {
		t.Helper()
		updated, err := time.Parse(time.RFC3339, value)
		if err != nil || !strings.HasSuffix(value, "Z") || updated.Before(before.Truncate(time.Second)) || updated.After(time.Now()) {
			t.Fatalf("expected a current UTC update time, got %q: %v", value, err)
		}
	}
	before := time.Now()
	if response := categoryRequest(t, server, token, http.MethodPost, collection, `{"title":"新建草稿","updated_at":"2024-03-02T12:34:00Z"}`); response.Code != http.StatusOK {
		t.Fatalf("create draft returned %d: %s", response.Code, response.Body.String())
	}
	page := list()
	if len(page.Items) != 2 || page.Items[0].Title != "新建草稿" || page.Items[0].PublishedAt != "" || page.Items[1].UpdatedAt != "" {
		t.Fatalf("unexpected content dates after creation: %+v", page.Items)
	}
	checkTime(page.Items[0].UpdatedAt, before)
	for _, update := range []struct{ query, title string }{
		{"", "已编辑的内容"},
		{"?lang=en&publish=1", "Edited translation"},
	} {
		// Start each edit from a known time so both saves can be verified without waiting for a clock tick.
		if _, err := database.Exec(`UPDATE gocms_content SET updated_at = '2024-03-02T12:34:00Z' WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		before = time.Now()
		payload := fmt.Sprintf(`{"title":%q,"visible":1,"published_at":%q}`, update.title, publishedAt)
		response := categoryRequest(t, server, token, http.MethodPut, collection+"/1"+update.query, payload)
		if response.Code != http.StatusOK {
			t.Fatalf("save returned %d: %s", response.Code, response.Body.String())
		}
		page = list()
		if len(page.Items) != 2 || page.Items[1].ID != 1 || page.Items[1].PublishedAt != publishedAt {
			t.Fatalf("unexpected publication date after save: %+v", page.Items)
		}
		checkTime(page.Items[1].UpdatedAt, before)
	}
}

// 列表筛选默认只匹配所选栏目；include_children=1 时按 "parent_id" 递归覆盖下级栏目。
func TestAdminContentCategoryFilterIncludesDescendants(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	parent := createContentTestCategory(t, server, database, token, "父栏目", 0)
	child := createContentTestCategory(t, server, database, token, "子栏目", parent)
	grandchild := createContentTestCategory(t, server, database, token, "孙栏目", child)
	sibling := createContentTestCategory(t, server, database, token, "其他栏目", 0)
	insertContentTestRow(t, database, "父栏目内容", parent, 1)
	insertContentTestRow(t, database, "子栏目内容", child, 2)
	insertContentTestRow(t, database, "孙栏目内容", grandchild, 3)
	insertContentTestRow(t, database, "其他栏目内容", sibling, 4)

	cases := []struct {
		name            string
		categoryID      int64
		includeChildren string
		want            []string
	}{
		{
			name:       "父栏目默认仅本级",
			categoryID: parent,
			want:       []string{"父栏目内容"},
		},
		{
			name:            "父栏目显式关闭下级",
			categoryID:      parent,
			includeChildren: "0",
			want:            []string{"父栏目内容"},
		},
		{
			name:            "父栏目包含全部下级",
			categoryID:      parent,
			includeChildren: "1",
			want:            []string{"父栏目内容", "子栏目内容", "孙栏目内容"},
		},
		{
			name:            "子栏目包含其下级",
			categoryID:      child,
			includeChildren: "true",
			want:            []string{"子栏目内容", "孙栏目内容"},
		},
		{
			name:            "叶子栏目不受影响",
			categoryID:      grandchild,
			includeChildren: "1",
			want:            []string{"孙栏目内容"},
		},
		{
			name:            "未选择栏目时返回全部",
			categoryID:      0,
			includeChildren: "1",
			want:            []string{"其他栏目内容", "子栏目内容", "孙栏目内容", "父栏目内容"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			page, titles := listContentTitles(t, server, token, testCase.categoryID, testCase.includeChildren)
			sort.Strings(testCase.want)
			if got := strings.Join(titles, ","); got != strings.Join(testCase.want, ",") {
				t.Fatalf("titles = %q, want %q", got, strings.Join(testCase.want, ","))
			}
			if page.Total != int64(len(testCase.want)) {
				t.Fatalf("total = %d, want %d", page.Total, len(testCase.want))
			}
		})
	}
}

// 展开子栏目不得绕过栏目权限：受限管理员仍只能看到授权栏目内的内容。
func TestAdminContentCategoryFilterIncludesDescendantsRespectsScope(t *testing.T) {
	server, database, rootToken := newCategoryTestServer(t)
	parent := createContentTestCategory(t, server, database, rootToken, "父栏目", 0)
	child := createContentTestCategory(t, server, database, rootToken, "子栏目", parent)
	grandchild := createContentTestCategory(t, server, database, rootToken, "孙栏目", child)
	insertContentTestRow(t, database, "父栏目内容", parent, 1)
	insertContentTestRow(t, database, "子栏目内容", child, 2)
	insertContentTestRow(t, database, "孙栏目内容", grandchild, 3)

	if _, err := database.Exec(`INSERT INTO gocms_admin_group (name, permissions) VALUES ('scoped-child', '["content"]');
		INSERT INTO gocms_admin_user (username, group_id) SELECT 'scoped-child', id FROM gocms_admin_group WHERE name = 'scoped-child'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE gocms_admin_user SET category_ids = ? WHERE username = 'scoped-child'`, fmt.Sprintf("[%d]", child)); err != nil {
		t.Fatal(err)
	}
	token, err := server.createSession("scoped-child")
	if err != nil {
		t.Fatal(err)
	}

	page, titles := listContentTitles(t, server, token, parent, "1")
	if got := strings.Join(titles, ","); got != "子栏目内容" {
		t.Fatalf("scoped titles = %q, want %q", got, "子栏目内容")
	}
	if page.Total != 1 {
		t.Fatalf("scoped total = %d, want 1", page.Total)
	}
}

func createContentTestCategory(t *testing.T, server *Server, database *sql.DB, token, name string, parentID int64) int64 {
	t.Helper()
	payload := fmt.Sprintf(`{"name":%q,"parent_id":%d,"list_template":"category_list.html","detail_template":"content_detail.html"}`, name, parentID)
	response := categoryRequest(t, server, token, http.MethodPost, "/api/admin/categories", payload)
	if response.Code != http.StatusOK {
		t.Fatalf("create category %q returned %d: %s", name, response.Code, response.Body.String())
	}
	return categoryID(t, database, name)
}

func insertContentTestRow(t *testing.T, database *sql.DB, title string, categoryID, order int64) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO "gocms_content" ("category_id", "route_key", "title", "sort_order", "visible")
		VALUES (?, ?, ?, ?, 1)`, categoryID, fmt.Sprintf("route-%d", order), title, order); err != nil {
		t.Fatal(err)
	}
}

func listContentTitles(t *testing.T, server *Server, token string, categoryID int64, includeChildren string) (ContentPage, []string) {
	t.Helper()
	params := url.Values{}
	params.Set("page_size", "100")
	if categoryID > 0 {
		params.Set("category_id", fmt.Sprintf("%d", categoryID))
	}
	if includeChildren != "" {
		params.Set("include_children", includeChildren)
	}
	response := categoryRequest(t, server, token, http.MethodGet, "/api/admin/content?"+params.Encode(), "")
	if response.Code != http.StatusOK {
		t.Fatalf("list content returned %d: %s", response.Code, response.Body.String())
	}
	var page ContentPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	titles := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		titles = append(titles, item.Title)
	}
	sort.Strings(titles)
	return page, titles
}
