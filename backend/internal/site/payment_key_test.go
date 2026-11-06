package site

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/commerce"
)

func TestPaymentKeyInitializePersistRestartAndRestore(t *testing.T) {
	t.Setenv("GOCMS_PAYMENT_KEY", "")
	s, database, root := newCategoryTestServer(t)
	dir := t.TempDir()
	if err := s.ConfigurePaymentKey(dir); err != nil {
		t.Fatal(err)
	}
	call := func(server *Server, method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := categoryRequest(t, server, root, method, "/api/admin/payments/"+path, body)
		if w.Code != want {
			t.Fatalf("%s %s = %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	status := call(s, "GET", "status", "", 200)
	if !strings.Contains(status.Body.String(), `"key_can_initialize":true`) {
		t.Fatal("super cannot initialize key")
	}
	call(s, "POST", "key", `{"mode":"import","key":"weak"}`, 400)
	response := call(s, "POST", "key", `{"mode":"generate"}`, 200)
	keyFile := filepath.Join(dir, "payment.key")
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(raw))
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		t.Fatal("invalid generated key")
	}
	info, err := os.Stat(keyFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key file is not private")
	}
	if strings.Contains(response.Body.String(), key) {
		t.Fatal("key returned by initialize")
	}
	status = call(s, "GET", "status", "", 200)
	if strings.Contains(status.Body.String(), key) || !strings.Contains(status.Body.String(), `"key_source":"file"`) {
		t.Fatal("key leaked or wrong source")
	}
	call(s, "POST", "key", `{"mode":"generate"}`, 409)
	a, err := s.commerce.CreateAccount(context.Background(), 1, commerce.Account{Name: "test", Provider: "pingpong", Environment: "sandbox", ClientID: "test-client", MerchantID: "test-shop", Secret: "private-salt", Currency: "USD", PublicURL: "https://example.test"})
	if err != nil {
		t.Fatal("new key not immediately usable:", err)
	}
	restarted, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ConfigurePaymentKey(dir); err != nil {
		t.Fatal(err)
	}
	var encrypted string
	if err := database.QueryRow(`SELECT secret_ciphertext FROM gocms_payment_account WHERE id=?`, a.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.commerce.Cipher.Open(encrypted, "1:"+a.PublicID); err != nil {
		t.Fatal("restart lost key", err)
	}
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	restored, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.ConfigurePaymentKey(dir); err != nil {
		t.Fatal(err)
	}
	status = call(restored, "GET", "status", "", 200)
	if !strings.Contains(status.Body.String(), `"key_requires_restore":true`) {
		t.Fatal("missing key not recognized")
	}
	call(restored, "POST", "key", `{"mode":"generate"}`, 409)
	wrong := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	body, _ := json.Marshal(map[string]string{"mode": "import", "key": wrong})
	call(restored, "POST", "key", string(body), 400)
	if restored.commerce.Cipher.Configured() {
		t.Fatal("wrong restore key installed")
	}
	body, _ = json.Marshal(map[string]string{"mode": "import", "key": key})
	call(restored, "POST", "key", string(body), 200)
	if _, err := restored.commerce.Cipher.Open(encrypted, "1:"+a.PublicID); err != nil {
		t.Fatal("restore failed", err)
	}
	t.Setenv("GOCMS_PAYMENT_KEY", wrong)
	badEnv, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := badEnv.ConfigurePaymentKey(dir); err != nil {
		t.Fatal("persisted key did not take precedence over environment", err)
	}
	t.Setenv("GOCMS_PAYMENT_KEY", key)
	env, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := env.ConfigurePaymentKey(dir); err != nil {
		t.Fatal(err)
	}
	status = call(env, "GET", "status", "", 200)
	if !strings.Contains(status.Body.String(), `"key_source":"file"`) {
		t.Fatal("persisted file did not take precedence")
	}
}

func TestPaymentKeyPermissionAndFilesystemBoundaries(t *testing.T) {
	t.Setenv("GOCMS_PAYMENT_KEY", "")
	s, database, root := newCategoryTestServer(t)
	dir := t.TempDir()
	if err := s.ConfigurePaymentKey(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('key-manager','["payments","payments.configure"]'); INSERT INTO gocms_admin_user(username,group_id) SELECT 'key-manager',id FROM gocms_admin_group WHERE name='key-manager'`); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.createSession("key-manager")
	if err != nil {
		t.Fatal(err)
	}
	w := categoryRequest(t, s, viewer, "POST", "/api/admin/payments/key", `{"mode":"generate"}`)
	if w.Code != 403 {
		t.Fatal("ordinary admin initialized global key")
	}
	for _, origin := range []string{"https://evil.example"} {
		r := httptest.NewRequest("POST", "/api/admin/payments/key", strings.NewReader(`{"mode":"generate"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.AddCookie(&http.Cookie{Name: "gocms_admin", Value: root})
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-origin key initialization allowed")
		}
	}
	existing := filepath.Join(dir, "payment.key")
	if err := os.WriteFile(existing, []byte("do-not-overwrite"), 0600); err != nil {
		t.Fatal(err)
	}
	w = categoryRequest(t, s, root, "POST", "/api/admin/payments/key", `{"mode":"generate"}`)
	if w.Code != 503 || s.commerce.Cipher.Configured() {
		t.Fatal("failed persistence installed key")
	}
	raw, err := os.ReadFile(existing)
	if err != nil || string(raw) != "do-not-overwrite" {
		t.Fatal("existing key overwritten")
	}
	other, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.ConfigurePaymentKey(filepath.Join(other.siteRoot, "data")); err == nil {
		t.Fatal("public directory accepted")
	}
	if err := other.ConfigurePaymentKey(dir); err == nil {
		t.Fatal("corrupted key file accepted")
	}
	if err := os.Chmod(existing, 0644); err != nil {
		t.Fatal(err)
	}
	if err := other.ConfigurePaymentKey(dir); err == nil {
		t.Fatal("world-readable key file accepted")
	}
}
