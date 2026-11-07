package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocms/internal/db"
	"gocms/internal/member"
	"gocms/internal/memberoauth"
)

func TestPlatformGoogleSharedAcrossSites(t *testing.T) {
	s, database, admin := newCategoryTestServer(t)
	if _, err := database.Exec(`UPDATE gocms_site SET domain='alpha.example.test' WHERE id=1;
 INSERT INTO gocms_site(name,code,domain,aliases) VALUES('Beta','beta','beta.example.test','["alias.beta.example.test"]');
 INSERT INTO gocms_user_group(site_id,name,slug,is_default) VALUES(1,'Alpha default','alpha-default',1),(2,'Beta default','beta-default',1),(1,'Alpha private','alpha-private',0)`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureMemberLoginKey(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	request := func(host, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		// The selected admin site must never redirect a public auth request to another tenant.
		r.Header.Set("X-Site-Id", "2")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	ac := &http.Cookie{Name: "gocms_admin", Value: admin}
	if w := request("alpha.example.test", "POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	body := `{"enabled":true,"auto_register":true,"client_id":"shared.apps.googleusercontent.com","secret":"shared-secret","revision":""}`
	if w := request("alpha.example.test", "PUT", "/api/admin/member-login?site_id=1", body, ac); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, host := range []string{"alpha.example.test", "beta.example.test", "alias.beta.example.test"} {
		w := request(host, "GET", "/api/auth/providers", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"google":true`) {
			t.Fatalf("provider %s: %s", host, w.Body.String())
		}
	}
	if w := request("unknown.example.test", "GET", "/api/auth/providers", ""); strings.Contains(w.Body.String(), `"google":true`) {
		t.Fatal("unknown host advertised shared login")
	}
	// Both site selectors read the same platform credentials and callback directory.
	for _, site := range []string{"1", "2"} {
		w := request("alpha.example.test", "GET", "/api/admin/member-login?site_id="+site, "", ac)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "https://beta.example.test/api/auth/google/callback") || !strings.Contains(w.Body.String(), "shared.apps.googleusercontent.com") {
			t.Fatal(w.Body.String())
		}
	}
	var nonce string
	s.googleIdentity = func(ctx context.Context, c memberoauth.Config, secret, code, verifier string) (googleClaims, error) {
		if secret != "shared-secret" || c.ClientID != "shared.apps.googleusercontent.com" {
			t.Fatal("not using shared credentials")
		}
		return googleClaims{Subject: "same-google-person", Email: "shared@example.test", Name: "Shared", Verified: true, Nonce: nonce}, nil
	}
	start := func(host string) (string, *http.Cookie) {
		t.Helper()
		w := request(host, "POST", "/api/auth/google/start?site_id=2", `{"next":"/shop/orders"}`)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var result map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(result["url"])
		if err != nil {
			t.Fatal(err)
		}
		if got := u.Query().Get("redirect_uri"); got != "https://"+host+memberoauth.CallbackPath {
			t.Fatalf("callback %s", got)
		}
		nonce = u.Query().Get("nonce")
		return u.Query().Get("state"), w.Result().Cookies()[0]
	}
	callback := func(host, state string, cookie *http.Cookie) *http.Cookie {
		t.Helper()
		w := request(host, "GET", memberoauth.CallbackPath+"?state="+state+"&code=test", "", cookie)
		if w.Header().Get("Location") != "/shop/orders" {
			t.Fatal(w.Header().Get("Location"))
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == "gocms_user" {
				return c
			}
		}
		t.Fatal("no member session")
		return nil
	}
	state, browser := start("alpha.example.test")
	aCookie := callback("alpha.example.test", state, browser)
	var id int64
	if err := database.QueryRow(`SELECT id FROM gocms_user WHERE email='shared@example.test'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_user_group_member(user_id,group_id) SELECT ?,id FROM gocms_user_group WHERE slug='alpha-private'`, id); err != nil {
		t.Fatal(err)
	}
	state, browser = start("beta.example.test")
	bCookie := callback("beta.example.test", state, browser)
	var users, identities, groups int
	var sites string
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_user`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_platform_identity`).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT site_ids FROM gocms_user WHERE id=?`, id).Scan(&sites); err != nil {
		t.Fatal(err)
	}
	if users != 1 || identities != 1 || sites != "[1,2]" {
		t.Fatalf("users=%d identities=%d sites=%s", users, identities, sites)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_user_group_member WHERE user_id=?`, id).Scan(&groups); err != nil || groups != 3 {
		t.Fatalf("default memberships: %d %v", groups, err)
	}
	for host, cookie := range map[string]*http.Cookie{"alpha.example.test": aCookie, "beta.example.test": bCookie} {
		w := request(host, "GET", "/api/v1/me/memberships", "", cookie)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		identity := request(host, "GET", "/api/v1/me/identities/google", "", cookie)
		if identity.Code != 200 || !strings.Contains(identity.Body.String(), `"bound":true`) {
			t.Fatal(identity.Body.String())
		}
		if host == "beta.example.test" && strings.Contains(w.Body.String(), "alpha-private") {
			t.Fatal("site A entitlement leaked into B")
		}
	}
	// Even a supplied browser cookie cannot move A's authorization transaction to B.
	state, browser = start("alpha.example.test")
	w := request("beta.example.test", "GET", memberoauth.CallbackPath+"?code=test&state="+state, "", browser)
	if !strings.Contains(w.Header().Get("Location"), "google_unavailable") {
		t.Fatal("cross-site callback accepted")
	}
	// One platform switch controls all sites and invalidates every pending transaction.
	state, browser = start("beta.example.test")
	c, err := memberoauth.Load(context.Background(), database, 1)
	if err != nil {
		t.Fatal(err)
	}
	disabled := `{"enabled":false,"auto_register":true,"client_id":"shared.apps.googleusercontent.com","revision":"` + c.Revision + `"}`
	if w = request("alpha.example.test", "PUT", "/api/admin/member-login", disabled, ac); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, host := range []string{"alpha.example.test", "beta.example.test"} {
		if w = request(host, "GET", "/api/auth/providers", ""); strings.Contains(w.Body.String(), `"google":true`) {
			t.Fatal("platform disabled but button available")
		}
	}
	w = request("beta.example.test", "GET", memberoauth.CallbackPath+"?code=test&state="+state, "", browser)
	if w.Header().Get("Location") == "/shop/orders" {
		t.Fatal("pending login survived platform disable")
	}
}

func TestPlatformGoogleLegacyConfigurationMigration(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "multiple"}[multiple], func(t *testing.T) {
			s, database, admin := newCategoryTestServer(t)
			ctx := context.Background()
			if err := s.ConfigureMemberLoginKey(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			if w := categoryRequest(t, s, admin, "POST", "/api/admin/member-login/key", `{"mode":"generate"}`); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			v := s.oauthVault
			encrypted, err := v.Seal(1, "old-secret-one")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = database.Exec(`INSERT INTO gocms_member_oauth_config(site_id,provider,enabled,auto_register,client_id,secret_ciphertext,public_url,revision) VALUES(1,'google',1,1,'old.apps.googleusercontent.com',?,'https://old.example.test','old-one')`, encrypted); err != nil {
				t.Fatal(err)
			}
			if multiple {
				if _, err = database.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example.test')`); err != nil {
					t.Fatal(err)
				}
				encrypted, err = v.Seal(2, "old-secret-two")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = database.Exec(`INSERT INTO gocms_member_oauth_config(site_id,provider,enabled,auto_register,client_id,secret_ciphertext,revision) VALUES(2,'google',1,1,'other.apps.googleusercontent.com',?,'old-two')`, encrypted); err != nil {
					t.Fatal(err)
				}
			}
			if err = memberoauth.Ensure(ctx, database); err != nil {
				t.Fatal(err)
			}
			c, err := memberoauth.Load(ctx, database, 1)
			if err != nil {
				t.Fatal(err)
			}
			if multiple {
				if c.Revision != "" {
					t.Fatal("ambiguous legacy configuration was selected automatically")
				}
				w := categoryRequest(t, s, admin, "GET", "/api/admin/member-login", "")
				if !strings.Contains(w.Body.String(), `"legacy_configs":[{`) || strings.Contains(w.Body.String(), "old-secret") {
					t.Fatal(w.Body.String())
				}
				w = categoryRequest(t, s, admin, "POST", "/api/admin/member-login/migrate", `{"site_id":2,"revision":"old-two"}`)
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				c, err = memberoauth.Load(ctx, database, 1)
				if err != nil {
					t.Fatal(err)
				}
				if c.SecretScope != 2 || c.ClientID != "other.apps.googleusercontent.com" {
					t.Fatal("selected migration source lost")
				}
				secret, err := v.Open(c.SecretScope, c.Encrypted)
				if err != nil || secret != "old-secret-two" {
					t.Fatal("migrated secret unreadable")
				}
				if w = categoryRequest(t, s, admin, "POST", "/api/admin/member-login/migrate", `{"site_id":1,"revision":"old-one"}`); w.Code != 409 {
					t.Fatal("migration overwrote a platform configuration")
				}
			} else {
				if c.SecretScope != 1 || c.ClientID != "old.apps.googleusercontent.com" || !c.Enabled {
					t.Fatal("single configuration not adopted")
				}
				secret, err := v.Open(c.SecretScope, c.Encrypted)
				if err != nil || secret != "old-secret-one" {
					t.Fatal("adopted secret unreadable")
				}
			}
			before := c.Revision
			if err = memberoauth.Ensure(ctx, database); err != nil {
				t.Fatal(err)
			}
			c, err = memberoauth.Load(ctx, database, 2)
			if err != nil || c.Revision != before {
				t.Fatal("migration is not idempotent")
			}
		})
	}
}

func TestPlatformGoogleLegacyIdentityMigration(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	ctx := context.Background()
	svc := member.Service{DB: database}
	if _, err := database.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example.test');INSERT INTO gocms_platform_oauth_config(provider,enabled,auto_register,client_id,revision) VALUES('google',1,1,'client','revision')`); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Register(ctx, "legacy-person", "legacy@example.test", "password123", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO gocms_member_identity(site_id,user_id,provider,issuer,subject) VALUES(1,?,'google',?,'legacy-subject')`, p.ID, memberoauth.Issuer); err != nil {
		t.Fatal(err)
	}
	got, token, err := svc.ExternalLogin(ctx, 2, "revision", "legacy-subject", "changed@example.test", "Name", true, 0, "", "ip", "agent", "")
	if err != nil || got.ID != p.ID {
		t.Fatalf("legacy promotion: %v", err)
	}
	{
		bound, _, err := svc.ExternalStatus(ctx, p.ID)
		if err != nil || !bound {
			t.Fatal("global identity unavailable on a site")
		}
	}
	if err = svc.UnbindGoogle(ctx, p.ID, token, "password123"); err != nil {
		t.Fatal(err)
	}
	{
		bound, _, err := svc.ExternalStatus(ctx, p.ID)
		if err != nil || bound {
			t.Fatal("global unbind was not applied to every site")
		}
	}
	// Conflicting legacy bindings are retained for authenticated owners to unlink;
	// site ordering and matching email must never select one of the accounts.
	other, err := svc.Register(ctx, "other-person", "other@example.test", "password123", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO gocms_member_identity(site_id,user_id,provider,issuer,subject) VALUES(1,?,'google',?,'conflicting'),(2,?,'google',?,'conflicting')`, p.ID, memberoauth.Issuer, other.ID, memberoauth.Issuer); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.ExternalLogin(ctx, 2, "revision", "conflicting", "legacy@example.test", "Name", true, 0, "", "ip", "agent", ""); err != member.ErrConflict {
		t.Fatal("conflicting legacy accounts merged")
	}
	// Deleting a site must not delete shared platform settings or identity tables.
	if err = db.DeleteSite(ctx, s.database, 2); err != nil {
		t.Fatal(err)
	}
	c, err := memberoauth.Load(ctx, database, 1)
	if err != nil || c.Revision != "revision" {
		t.Fatal("site deletion removed platform configuration")
	}
}

func TestPlatformGoogleCallbackDirectory(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	ctx := context.Background()
	if _, err := database.Exec(`UPDATE gocms_site SET domain='example.test',aliases='["alias.example.test","localhost","*.wild.example.test"]' WHERE id=1;
 INSERT INTO gocms_site(name,code,domain,status) VALUES('Disabled','disabled','disabled.example.test','disabled');
 INSERT INTO gocms_language(site_id,code,name,site_mode,domain) VALUES(1,'fr','French','standalone','fr.example.test')`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSiteSettingsForSite(ctx, database, 1, map[string]string{"site_url": "http://localhost:18766"}); err != nil {
		t.Fatal(err)
	}
	callbacks, err := s.memberLoginCallbacks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	urls := map[string]bool{}
	for _, callback := range callbacks {
		urls[callback.URL] = true
		if callback.SiteID != 1 || strings.Contains(callback.URL, "*") {
			t.Fatal("disabled or wildcard callback included")
		}
	}
	for _, origin := range []string{"https://example.test", "https://alias.example.test", "https://fr.example.test", "http://localhost:18766"} {
		if !urls[origin+memberoauth.CallbackPath] {
			t.Errorf("callback not listed for %s", origin)
		}
	}
	if _, err = s.googleConfigForOrigin(ctx, "http://localhost:18767"); err == nil {
		t.Fatal("unconfigured port accepted")
	}
	if err = db.SaveSiteSettingsForSite(ctx, database, 1, map[string]string{"site_url": "https://unmanaged.example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.googleConfigForOrigin(ctx, "https://unmanaged.example.test"); err == nil {
		t.Fatal("unmanaged site_url accepted")
	}
}
