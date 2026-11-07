package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocms/internal/member"
	"gocms/internal/membermail"
	"gocms/internal/memberoauth"
)

func TestPasswordRecoveryHTTPAndMailAdmin(t *testing.T) {
	s, database, admin := newCategoryTestServer(t)
	if _, e := database.Exec(`UPDATE gocms_site SET domain='example.com' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfigureMemberLoginKey(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	request := func(host, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("code %d want %d: %s", w.Code, code, w.Body.String())
		}
	}
	ac := &http.Cookie{Name: "gocms_admin", Value: admin}
	check(request("example.com", "PUT", "/api/admin/mail", `{"enabled":false,"support_email":"help@example.com","port":587,"tls":"starttls"}`, ac), 200)
	w := request("example.com", "GET", "/api/v1/auth/password-options", "")
	check(w, 200)
	if !strings.Contains(w.Body.String(), `"support_email":"help@example.com"`) || !strings.Contains(w.Body.String(), `"email_enabled":false`) {
		t.Fatal(w.Body.String())
	}
	check(request("example.com", "POST", "/api/v1/auth/forgot-password", `{"email":"person@example.com"}`), 503)
	check(request("example.com", "POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac), 200)
	c, e := membermail.Load(context.Background(), database)
	if e != nil {
		t.Fatal(e)
	}
	body := fmt.Sprintf(`{"enabled":true,"support_email":"help@example.com","host":"smtp.example.com","port":587,"tls":"starttls","username":"smtp-user","password":"smtp-secret","from_address":"sender@example.com","from_name":"Support","revision":%q}`, c.Revision)
	check(request("example.com", "PUT", "/api/admin/mail", body, ac), 200)
	check(request("example.com", "PUT", "/api/admin/mail", body, ac), 409)
	w = request("example.com", "GET", "/api/admin/mail", "", ac)
	check(w, 200)
	if strings.Contains(w.Body.String(), "smtp-secret") || strings.Contains(w.Body.String(), "secret_ciphertext") {
		t.Fatal("secret leaked")
	}
	c, e = membermail.Load(context.Background(), database)
	if e != nil {
		t.Fatal(e)
	}
	if c.Encrypted == "" || c.Encrypted == "smtp-secret" {
		t.Fatal("unencrypted secret")
	}
	if _, e = s.oauthVault.Open(0, c.Encrypted); e == nil {
		t.Fatal("cross-scope secret accepted")
	}
	if e = s.validateLoginKey(context.Background(), s.oauthVault); e != nil {
		t.Fatal("SMTP key validation failed", e)
	}
	type sentMail struct{ to, body string }
	sent := make(chan sentMail, 10)
	s.mailSend = func(ctx context.Context, config membermail.Config, password, to, subject, body string) error {
		if password != "smtp-secret" || config.Host != "smtp.example.com" {
			t.Error("missing credentials")
		}
		sent <- sentMail{to, body}
		return nil
	}
	check(request("example.com", "POST", "/api/admin/mail/test", `{"email":"test@example.com"}`, ac), 200)
	<-sent
	svc := member.Service{DB: database}
	p, e := svc.Register(context.Background(), "recover", "person@example.com", "password123", 1)
	if e != nil {
		t.Fatal(e)
	}
	_, session, e := svc.Login(context.Background(), p.Username, "password123", "", "", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	known := request("example.com", "POST", "/api/v1/auth/forgot-password", `{"email":"person@example.com","language":"zh-CN"}`)
	check(known, 200)
	var delivery sentMail
	select {
	case delivery = <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("mail not delivered")
	}
	i := strings.Index(delivery.body, "#token=")
	if i < 0 || delivery.to != "person@example.com" {
		t.Fatal(delivery)
	}
	token := strings.TrimSpace(delivery.body[i+7:])
	if len(token) != 43 {
		t.Fatal("missing token")
	}
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for len(s.resetJobs) > 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if len(s.resetJobs) > 0 {
			t.Fatal("mail worker did not finish")
		}
	}
	wait()
	unknown := request("example.com", "POST", "/api/v1/auth/forgot-password", `{"email":"unknown@example.com","language":"zh-CN"}`)
	check(unknown, 200)
	wait()
	if known.Body.String() != unknown.Body.String() || strings.Contains(known.Body.String(), token) {
		t.Fatal("account existence or token leaked")
	}
	select {
	case <-sent:
		t.Fatal("unknown account received email")
	default:
	}
	check(request("evil.example", "POST", "/api/v1/auth/forgot-password", `{"email":"person@example.com"}`), 403)
	check(request("example.com", "POST", "/api/v1/auth/reset-password", fmt.Sprintf(`{"token":%q,"password":"newpassword123"}`, token)), 200)
	if _, e = svc.Authenticate(context.Background(), session, 1); e == nil {
		t.Fatal("old session survived reset")
	}
	_, session, e = svc.Login(context.Background(), p.Username, "newpassword123", "", "", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	mc := &http.Cookie{Name: "gocms_user", Value: session}
	check(request("example.com", "PUT", "/api/v1/me/password", `{"old_password":"wrong","new_password":"newpassword456"}`, mc), 401)
	check(request("example.com", "PUT", "/api/v1/me/password", `{"old_password":"newpassword123","new_password":"newpassword456"}`, mc), 200)
	if _, e = svc.Authenticate(context.Background(), session, 1); e == nil {
		t.Fatal("old session survived change")
	}
	if _, e = database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('mail-viewer','["members","mail"]');INSERT INTO gocms_admin_user(username,group_id) SELECT 'mail-viewer',id FROM gocms_admin_group WHERE name='mail-viewer'`); e != nil {
		t.Fatal(e)
	}
	viewer, e := s.createSession("mail-viewer")
	if e != nil {
		t.Fatal(e)
	}
	vc := &http.Cookie{Name: "gocms_admin", Value: viewer}
	for _, method := range []string{"GET", "PUT"} {
		check(request("example.com", method, "/api/admin/mail", body, vc), 403)
	}
	check(request("example.com", "POST", "/api/admin/mail/test", `{"email":"test@example.com"}`, vc), 403)
}
func TestBoundProviderPasswordRecoveryHTTP(t *testing.T) {
	mockFacebook(t)
	s, database, admin := newCategoryTestServer(t)
	ctx := context.Background()
	database.Exec(`UPDATE gocms_site SET domain='example.com' WHERE id=1`)
	if e := s.ConfigureMemberLoginKey(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	ac := &http.Cookie{Name: "gocms_admin", Value: admin}
	if w := request("POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	svc := member.Service{DB: database}
	p, e := svc.Register(ctx, "linked", "", "password123", 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = database.Exec(`UPDATE gocms_user SET password_hash='' WHERE id=?;INSERT INTO gocms_platform_identity(user_id,provider,issuer,subject) VALUES(?,'google','https://accounts.google.com','linked-sub'),(?,'facebook','https://www.facebook.com/123456','fb-person')`, p.ID, p.ID, p.ID); e != nil {
		t.Fatal(e)
	}
	subject, nonce := "linked-sub", ""
	s.googleIdentity = func(ctx context.Context, c memberoauth.Config, secret, code, verifier string) (googleClaims, error) {
		return googleClaims{Subject: subject, Nonce: nonce}, nil
	}
	for _, provider := range []string{"google", "facebook"} {
		t.Run(provider, func(t *testing.T) {
			client, secret := "test.apps.googleusercontent.com", "secret"
			if provider == "facebook" {
				client = "123456"
				secret = "facebook-secret"
			}
			body := fmt.Sprintf(`{"enabled":true,"auto_register":true,"client_id":%q,"secret":%q}`, client, secret)
			if w := request("PUT", "/api/admin/member-login?provider="+provider, body, ac); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			w := request("POST", "/api/auth/"+provider+"/start", `{"recover":true}`)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			var out map[string]string
			json.Unmarshal(w.Body.Bytes(), &out)
			u, e := url.Parse(out["url"])
			if e != nil {
				t.Fatal(e)
			}
			nonce = u.Query().Get("nonce")
			state := u.Query().Get("state")
			cookie := w.Result().Cookies()[0]
			w = request("GET", "/api/auth/"+provider+"/callback?code=code&state="+state, "", cookie)
			location, e := url.Parse(w.Header().Get("Location"))
			if e != nil || location.Path != "/shop/reset-password" {
				t.Fatalf("no grant %v %s", e, w.Header())
			}
			token := strings.TrimPrefix(location.Fragment, "token=")
			for _, ck := range w.Result().Cookies() {
				if ck.Name == "gocms_user" && ck.Value != "" {
					t.Fatal("recovery created login session")
				}
			}
			w = request("POST", "/api/v1/auth/reset-password", fmt.Sprintf(`{"token":%q,"password":"fromprovider123"}`, token))
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
		})
	}
	// Unbound Google identity cannot register a new account through recovery.
	subject = "never-bound"
	w := request("POST", "/api/auth/google/start", `{"recover":true}`)
	var out map[string]string
	json.Unmarshal(w.Body.Bytes(), &out)
	u, _ := url.Parse(out["url"])
	nonce = u.Query().Get("nonce")
	cookie := w.Result().Cookies()[0]
	w = request("GET", "/api/auth/google/callback?code=code&state="+u.Query().Get("state"), "", cookie)
	if !strings.Contains(w.Header().Get("Location"), "recovery_error=1") {
		t.Fatal("unbound recovery accepted")
	}
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM gocms_user`).Scan(&count)
	if count != 1 {
		t.Fatal("recovery registered member")
	}
}
