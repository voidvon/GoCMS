package site

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"gocms/internal/db"
	"gocms/internal/member"
	"gocms/internal/memberoauth"
	"golang.org/x/oauth2"
)

func loginProvider(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/auth/facebook/") || (strings.HasPrefix(r.URL.Path, "/api/admin/member-login") && r.URL.Query().Get("provider") == "facebook") {
		return "facebook"
	}
	return "google"
}
func (s *Server) providerSite(r *http.Request) (memberoauth.Config, error) {
	c, e := s.googleSite(r)
	if e != nil {
		return c, e
	}
	if loginProvider(r) == "facebook" {
		c, e = memberoauth.LoadProvider(r.Context(), s.database, c.SiteID, "facebook")
		c.PublicURL = requestScheme(r) + "://" + strings.ToLower(requestHost(r))
	}
	return c, e
}

type googleClaims struct {
	Subject         string `json:"sub"`
	Email           string `json:"email"`
	Verified        bool   `json:"email_verified"`
	Name            string `json:"name"`
	Nonce           string `json:"nonce"`
	AuthorizedParty string `json:"azp"`
}

var googleHTTPClient = &http.Client{Timeout: 15 * time.Second}

// A shared key set caches and refreshes Google's signing keys across requests.
var googleKeySet = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), googleHTTPClient), "https://www.googleapis.com/oauth2/v3/certs")

func googleOAuth(c memberoauth.Config, secret string) oauth2.Config {
	return oauth2.Config{ClientID: c.ClientID, ClientSecret: secret, RedirectURL: c.Callback(), Scopes: []string{oidc.ScopeOpenID, "email", "profile"}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}}
}
func verifyGoogle(ctx context.Context, c memberoauth.Config, secret, code, verifier string) (googleClaims, error) {
	ctx = oidc.ClientContext(ctx, googleHTTPClient)
	config := googleOAuth(c, secret)
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return googleClaims{}, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return googleClaims{}, errors.New("missing id token")
	}
	v := oidc.NewVerifier(memberoauth.Issuer, googleKeySet, &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{"RS256"}})
	id, err := v.Verify(ctx, raw)
	if err != nil {
		return googleClaims{}, err
	}
	var claims googleClaims
	err = id.Claims(&claims)
	if err == nil && claims.AuthorizedParty != "" && claims.AuthorizedParty != c.ClientID {
		return googleClaims{}, errors.New("unexpected authorized party")
	}
	return claims, err
}
func safeOAuthNext(value string) string {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n\t ") {
		return "/shop/cart"
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return "/shop/cart"
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Host != "" || u.Scheme != "" {
		return "/shop/cart"
	}
	u.Path = path.Clean(u.Path)
	u.RawPath = ""
	if strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\r\n\t") {
		return "/shop/cart"
	}
	p := strings.ToLower(u.Path)
	for _, prefix := range []string{"/api", "/admin", "/shop/login", "/shop/register", "/shop/forgot-password", "/shop/reset-password", "/shop/cart/add"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return "/shop/cart"
		}
	}
	return u.String()
}
func (s *Server) googleSite(r *http.Request) (memberoauth.Config, error) {
	return s.googleConfigForOrigin(r.Context(), requestScheme(r)+"://"+strings.ToLower(requestHost(r)))
}
func googleOrigin(c memberoauth.Config, r *http.Request) bool {
	u, e := url.Parse(c.PublicURL)
	return e == nil && u.Host != "" && strings.EqualFold(u.Host, requestHost(r)) && u.Scheme == requestScheme(r)
}
func (s *Server) googleAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.database == nil {
		memberError(w, 503, "unavailable", "Login unavailable")
		return
	}
	if r.URL.Path == "/api/auth/"+loginProvider(r)+"/callback" {
		s.googleCallback(w, r)
		return
	}
	if r.URL.Path == "/api/auth/providers" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		c, e := s.providerSite(r)
		enabled := false
		if e == nil && c.Enabled && googleOrigin(c, r) {
			s.oauthMu.RLock()
			_, e = s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted)
			s.oauthMu.RUnlock()
			enabled = e == nil
		}
		writeJSON(w, 200, map[string]any{"google": enabled, "facebook": s.facebookAvailable(r)})
		return
	}
	if r.URL.Path != "/api/auth/"+loginProvider(r)+"/start" {
		http.NotFound(w, r)
		return
	}
	s.startGoogle(w, r, false)
}
func (s *Server) startGoogle(w http.ResponseWriter, r *http.Request, test bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !memberRequest(w, r) {
		return
	}
	var in struct {
		Recover  bool   `json:"recover"`
		Next     string `json:"next"`
		Bind     bool   `json:"bind"`
		Password string `json:"password"`
	}
	if memberDecode(r, &in) != nil {
		memberResult(w, nil, member.ErrInvalid)
		return
	}
	if in.Recover && (in.Bind || test) {
		memberResult(w, nil, member.ErrInvalid)
		return
	}
	c, err := s.providerSite(r)
	if err != nil || (!test && !c.Enabled) || !googleOrigin(c, r) {
		memberError(w, 403, loginProvider(r)+"_unavailable", "Provider login unavailable on this origin")
		return
	}
	check := c
	check.Enabled = true
	if err = memberoauth.Validate(check); err != nil {
		memberError(w, 503, loginProvider(r)+"_unavailable", "Provider configuration incomplete")
		return
	}
	s.oauthMu.RLock()
	_, err = s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted)
	s.oauthMu.RUnlock()
	if err != nil {
		memberError(w, 503, loginProvider(r)+"_unavailable", "Provider configuration unavailable")
		return
	}
	if !s.memberAttempt(w, r, loginProvider(r)+"-start", "", 20) {
		return
	}
	user := int64(0)
	session := ""
	if test {
		u := s.currentAdmin(r)
		if u == nil {
			memberResult(w, nil, member.ErrUnauthorized)
			return
		}
		user = -u.ID
		session = memberoauth.Hash(adminSessionToken(r))
	} else if in.Bind {
		u, ok := s.currentSiteUser(r)
		cookie, e := r.Cookie("gocms_user")
		if !ok || e != nil {
			memberResult(w, nil, member.ErrUnauthorized)
			return
		}
		if e = (member.Service{DB: s.database}).ConfirmExternalChange(r.Context(), u.ID, cookie.Value, in.Password); e != nil {
			memberResult(w, nil, e)
			return
		}
		user = u.ID
		session = member.TokenHash(cookie.Value)
	}
	state, e := memberoauth.Random()
	if e != nil {
		memberResult(w, nil, e)
		return
	}
	browser, e := memberoauth.Random()
	if e != nil {
		memberResult(w, nil, e)
		return
	}
	nonce, e := memberoauth.Random()
	if e != nil {
		memberResult(w, nil, e)
		return
	}
	verifier := oauth2.GenerateVerifier()
	_, err = s.database.ExecContext(r.Context(), `DELETE FROM gocms_member_oauth_state WHERE expires_at<=?`, time.Now().Unix())
	if err == nil {
		_, err = s.database.ExecContext(r.Context(), `INSERT INTO gocms_member_oauth_state(state_hash,browser_hash,site_id,revision,nonce,verifier,next_path,user_id,session_hash,expires_at,origin,provider,recovery) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, memberoauth.Hash(state), memberoauth.Hash(browser), c.SiteID, c.Revision, nonce, verifier, safeOAuthNext(in.Next), user, session, time.Now().Add(10*time.Minute).Unix(), c.PublicURL, c.ProviderName(), in.Recover)
	}
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "gocms_oauth_" + state[:16], Value: browser, Path: "/api/auth/" + loginProvider(r) + "/callback", HttpOnly: true, Secure: requestScheme(r) == "https", SameSite: http.SameSiteLaxMode, MaxAge: 600})
	config := googleOAuth(c, "")
	target := config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "select_account"))
	if c.ProviderName() == "facebook" {
		fb := facebookOAuth(c, "")
		target = fb.AuthCodeURL(state)
	}
	writeJSON(w, 200, map[string]string{"url": target})
}

// Admin session cookie name is centralized in the existing session implementation.
func adminSessionToken(r *http.Request) string {
	c, e := r.Cookie("gocms_admin")
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	fail := func(code string) {
		code = strings.Replace(code, "google_", loginProvider(r)+"_", 1)
		http.Redirect(w, r, "/shop/login?oauth_error="+url.QueryEscape(code), http.StatusSeeOther)
	}
	state := r.URL.Query().Get("state")
	if len(state) != 43 {
		fail("google_failed")
		return
	}
	cookie, e := r.Cookie("gocms_oauth_" + state[:16])
	if e != nil {
		fail("google_failed")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookie.Name, Value: "", Path: "/api/auth/" + loginProvider(r) + "/callback", HttpOnly: true, Secure: requestScheme(r) == "https", SameSite: http.SameSiteLaxMode, MaxAge: -1})
	var recovery bool
	var site, user int64
	var revision, nonce, verifier, next, session, origin string
	// DELETE RETURNING atomically consumes the transaction and rejects replays.
	err := s.database.QueryRowContext(r.Context(), `DELETE FROM gocms_member_oauth_state WHERE state_hash=? AND browser_hash=? AND expires_at>? AND provider=? RETURNING site_id,revision,nonce,verifier,next_path,user_id,session_hash,origin,recovery`, memberoauth.Hash(state), memberoauth.Hash(cookie.Value), time.Now().Unix(), loginProvider(r)).Scan(&site, &revision, &nonce, &verifier, &next, &user, &session, &origin, &recovery)
	if err != nil {
		fail("google_failed")
		return
	}
	test := user < 0
	fail = func(code string) {
		code = strings.Replace(code, "google_", loginProvider(r)+"_", 1)
		if test {
			http.Redirect(w, r, adminLoginTestURL(r, "failed"), http.StatusSeeOther)
		} else if recovery {
			http.Redirect(w, r, "/shop/forgot-password?oauth_error="+url.QueryEscape(code), http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/shop/login?next="+url.QueryEscape(safeOAuthNext(next))+"&oauth_error="+url.QueryEscape(code), http.StatusSeeOther)
		}
	}
	c, err := s.providerSite(r)
	target, e := db.GetSiteByID(r.Context(), s.database, site)
	if err != nil || e != nil || target == nil || target.Status != "active" || (!test && !c.Enabled) || c.SiteID != site || c.PublicURL != origin || c.Revision != revision || !googleOrigin(c, r) {
		fail("google_unavailable")
		return
	}
	hostSite, e := db.GetSiteByHost(r.Context(), s.database, requestHost(r))
	if e != nil || hostSite == nil || hostSite.ID != site {
		fail("google_failed")
		return
	}
	if test {
		u, ok, isSession := s.authenticateRequest(r)
		if !ok || !isSession || u.ID != -user || memberoauth.Hash(adminSessionToken(r)) != session || !u.IsSuper {
			fail("google_failed")
			return
		}
	}
	if r.URL.Query().Get("error") != "" {
		fail("google_cancelled")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 8192 {
		fail("google_failed")
		return
	}
	s.oauthMu.RLock()
	secret, err := s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted)
	s.oauthMu.RUnlock()
	if err != nil {
		fail("google_unavailable")
		return
	}
	verify := s.googleIdentity
	if verify == nil {
		verify = verifyGoogle
	}
	var claims googleClaims
	if c.ProviderName() == "facebook" {
		claims, err = verifyFacebook(r.Context(), c, secret, code)
		claims.Nonce = nonce
	} else {
		claims, err = verify(r.Context(), c, secret, code, verifier)
	}
	if err != nil || claims.Nonce != nonce || claims.Subject == "" {
		fail("google_failed")
		return
	}
	if test {
		latest, e := memberoauth.LoadProvider(r.Context(), s.database, site, c.ProviderName())
		if e != nil || latest.Revision != revision {
			fail("google_failed")
			return
		}
		http.Redirect(w, r, adminLoginTestURL(r, "success"), http.StatusSeeOther)
		return
	}
	if recovery {
		token, e := (member.Service{DB: s.database}).IssueExternalPasswordReset(r.Context(), site, c.ProviderName(), revision, claims.Subject)
		if e != nil {
			http.Redirect(w, r, "/shop/forgot-password?recovery_error=1", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/shop/reset-password#token="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}

	current := ""
	if ck, e := r.Cookie("gocms_user"); e == nil {
		current = ck.Value
	}
	if user > 0 && member.TokenHash(current) != session {
		fail("google_failed")
		return
	}
	_, token, err := (member.Service{DB: s.database}).ExternalProviderLogin(r.Context(), site, c.ProviderName(), revision, claims.Subject, claims.Email, claims.Name, claims.Verified, user, session, clientIP(r), r.UserAgent(), current)
	if err != nil {
		switch {
		case errors.Is(err, member.ErrConflict):
			fail("google_conflict")
		case errors.Is(err, member.ErrSessionLimit):
			fail("session_limit_reached")
		default:
			fail("google_failed")
		}
		return
	}
	if token != "" {
		http.SetCookie(w, userCookie(token, 30*24*60*60, requestScheme(r) == "https"))
	}
	http.Redirect(w, r, safeOAuthNext(next), http.StatusSeeOther)
}

func (s *Server) memberGoogleIdentity(w http.ResponseWriter, r *http.Request, u siteUser) {
	svc := member.Service{DB: s.database}
	if r.Method == http.MethodGet {
		bound, password, e := svc.ExternalProviderStatus(r.Context(), u.ID, loginIdentityProvider(r))
		memberResult(w, map[string]bool{"bound": bound, "has_password": password}, e)
		return
	}
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if memberDecode(r, &in) != nil {
		memberResult(w, nil, member.ErrInvalid)
		return
	}
	cookie, e := r.Cookie("gocms_user")
	if e != nil {
		memberResult(w, nil, member.ErrUnauthorized)
		return
	}
	if !s.memberAttempt(w, r, loginIdentityProvider(r)+"-unbind", "", 10) {
		return
	}
	e = svc.UnbindProvider(r.Context(), u.ID, loginIdentityProvider(r), cookie.Value, in.Password)
	memberResult(w, map[string]bool{"ok": e == nil}, e)
}

func loginIdentityProvider(r *http.Request) string {
	if strings.HasSuffix(r.URL.Path, "/facebook") {
		return "facebook"
	}
	return "google"
}

func adminLoginTestURL(r *http.Request, result string) string {
	target := "/admin/member-login?oauth_test=" + result
	if loginProvider(r) == "facebook" {
		target += "&provider=facebook"
	}
	return target
}
