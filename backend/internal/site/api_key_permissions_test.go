package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"gocms/internal/apikey"
	"gocms/internal/commerce"
)

func apiKeyForTest(t *testing.T, s *Server, username string) *apikey.ApiKeyWithSecret {
	t.Helper()
	var id int64
	if err := s.database.QueryRow(`SELECT id FROM gocms_admin_user WHERE username=?`, username).Scan(&id); err != nil {
		t.Fatal(err)
	}
	key, err := apikey.Create(context.Background(), s.database, id, id, apikey.CreateInput{Name: "API permission test"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func apiKeyRequest(t *testing.T, s *Server, key, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-API-Key", key)
	r.Header.Set("X-Site-Id", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestAPIKeyAdministratorPermissions(t *testing.T) {
	t.Setenv("GOCMS_PAYMENT_KEY", "")
	s, database, _ := newCategoryTestServer(t)
	if err := s.ConfigurePaymentKey(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureMemberLoginKey(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureGeography(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.CloseGeography)
	if _, err := database.Exec(`INSERT INTO gocms_admin_user(username) VALUES('restricted')`); err != nil {
		t.Fatal(err)
	}
	root := apiKeyForTest(t, s, "gocms")
	restricted := apiKeyForTest(t, s, "restricted")
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/admin/users", "", 200},
		{"POST", "/api/admin/api-keys", `{"name":"API created key"}`, 201},
		{"GET", "/api/admin/groups", "", 200},
		{"POST", "/api/admin/groups", `{"name":"API managed","permissions":[]}`, 200},
		{"POST", "/api/admin/sites", `{"name":"API site","code":"api-site","domain":"api.example.test"}`, 201},
		{"GET", "/api/admin/site-users", "", 200},
		{"GET", "/api/admin/member-groups", "", 200},
		{"GET", "/api/admin/geography", "", 200},
		{"GET", "/api/admin/mail", "", 200},
		{"GET", "/api/admin/member-login", "", 200},
		{"POST", "/api/admin/member-login/key", `{"mode":"generate"}`, 200},
		{"POST", "/api/admin/payments/key", `{"mode":"generate"}`, 200},
		{"GET", "/api/admin/orders", "", 200},
		{"POST", "/api/admin/logs/clear", `{"before":"2000-01-01"}`, 200},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := apiKeyRequest(t, s, restricted.Key, tc.method, tc.path, tc.body)
			if w.Code != 403 {
				t.Fatalf("restricted administrator: %d %s", w.Code, w.Body.String())
			}
			w = apiKeyRequest(t, s, root.Key, tc.method, tc.path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("super administrator: %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, path := range []string{"/api/admin/payments/dimensions", "/api/admin/orders/missing/complete", "/api/admin/member-login/key"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", root.Key)
		r.Header.Set("Origin", "https://other.example.test")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin mutation %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestAPIKeySpecificationsAndPermissionBoundaries(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	permissions := []string{"content", "content.add", "content.edit", "content.review", "payments", "payments.configure"}
	setPermissions := func(t *testing.T, permissions []string) {
		t.Helper()
		raw, err := json.Marshal(map[string][]string{"1": permissions})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE gocms_admin_group SET site_permissions=? WHERE name='spec-editor'`, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example.test');
		INSERT INTO gocms_category(site_id,name) VALUES(1,'Catalog');
		INSERT INTO gocms_admin_group(name) VALUES('spec-editor');
		INSERT INTO gocms_admin_user(username,group_id) SELECT 'spec-editor',id FROM gocms_admin_group WHERE name='spec-editor'`); err != nil {
		t.Fatal(err)
	}
	setPermissions(t, permissions)
	key := apiKeyForTest(t, s, "spec-editor")
	call := func(t *testing.T, method, path string, payload any, want int) []byte {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		w := apiKeyRequest(t, s, key.Key, method, path, string(body))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var size, material commerce.DimensionRecord
	for _, d := range []*commerce.DimensionRecord{&size, &material} {
		name := "口径"
		if d == &material {
			name = "物料编码"
		}
		if err := json.Unmarshal(call(t, "POST", "/api/admin/payments/dimensions", map[string]any{"name": name, "type": "text"}, 201), d); err != nil {
			t.Fatal(err)
		}
	}
	size.Unit = "mm"
	sizePath := "/api/admin/payments/dimensions/" + size.Key
	updated := call(t, "PUT", sizePath, size, 200)
	call(t, "PUT", sizePath, size, 409) // Stale dictionary versions remain protected.
	if err := json.Unmarshal(updated, &size); err != nil {
		t.Fatal(err)
	}
	product := commerce.Product{Kind: "physical", Dimensions: []commerce.Dimension{size.Dimension, material.Dimension}, Variants: []commerce.Variant{{SKU: "864025012", Enabled: true, Specifications: []commerce.Specification{
		{Name: size.Name, Value: "25", Unit: size.Unit}, {Name: material.Name, Value: "864025012"},
	}}}}
	payload := map[string]any{"title": "Catalog item", "visible": 1, "category_id": categoryID(t, database, "Catalog"), "commerce": product}
	call(t, "POST", "/api/admin/content", payload, 200)
	var contentID int64
	if err := database.QueryRow(`SELECT id FROM gocms_content WHERE title='Catalog item'`).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/admin/content/%d", contentID)
	read := func() Content {
		t.Helper()
		var detail Content
		if err := json.Unmarshal(call(t, "GET", path, nil, 200), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Commerce == nil || len(detail.Commerce.Variants) != 1 {
			t.Fatalf("missing specifications: %+v", detail.Commerce)
		}
		return detail
	}
	product = *read().Commerce
	variantID := product.Variants[0].ID
	product.Dimensions = product.Dimensions[:1]
	product.Variants[0].Specifications = product.Variants[0].Specifications[:1]
	payload["commerce"] = product
	call(t, "PUT", path, payload, 200)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", path, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-API-Key", key.Key)
	r.Header.Set("Origin", "https://other.example.test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin specification edit: %d %s", w.Code, w.Body.String())
	}
	detail := read()
	if len(detail.Commerce.Dimensions) != 1 || len(detail.Commerce.Variants[0].Specifications) != 1 || detail.Commerce.Variants[0].SKU != "864025012" || detail.Commerce.Variants[0].ID != variantID || detail.Commerce.Enabled {
		t.Fatalf("specification cleanup changed SKU identity or sales state: %+v", detail.Commerce)
	}
	call(t, "DELETE", fmt.Sprintf("/api/admin/payments/dimensions/%s?version=%d", material.Key, material.Version), nil, 200)
	call(t, "GET", "/api/admin/payments/dimensions", nil, 200)
	for _, missing := range []string{"content", "content.edit", "content.review", "payments", "payments.configure"} {
		t.Run("without "+missing, func(t *testing.T) {
			var reduced []string
			for _, p := range permissions {
				if p != missing {
					reduced = append(reduced, p)
				}
			}
			setPermissions(t, reduced)
			defer setPermissions(t, permissions)
			if missing == "payments.configure" {
				call(t, "POST", "/api/admin/payments/dimensions", map[string]string{"name": "Other", "type": "text"}, 403)
				call(t, "PUT", path, payload, 200)
			} else {
				call(t, "PUT", path, payload, 403)
			}
		})
	}
	call(t, "PUT", path+"?site_id=2", payload, 403)
	call(t, "POST", "/api/admin/payments/dimensions?site_id=2", map[string]string{"name": "Other", "type": "text"}, 403)
	if _, err := database.Exec(`UPDATE gocms_admin_user SET category_ids='[]' WHERE username='spec-editor'`); err != nil {
		t.Fatal(err)
	}
	call(t, "PUT", path, payload, 403)
	if _, err := apikey.Revoke(context.Background(), database, key.ID, key.AdminID, "127.0.0.1", "test"); err != nil {
		t.Fatal(err)
	}
	call(t, "PUT", path, payload, 401)
}
