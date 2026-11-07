package site

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"gocms/internal/member"
	"gocms/internal/membermail"
)

func (s *Server) recoveryMail(r *http.Request) (membermail.Config, string, bool) {
	c, e := membermail.Load(r.Context(), s.database)
	if e != nil {
		return c, "", false
	}
	if !c.Enabled || membermail.Validate(c) != nil {
		return c, "", false
	}
	if c.Encrypted == "" {
		return c, "", true
	}
	s.oauthMu.RLock()
	secret, e := s.oauthVault.Open(membermail.SecretScope, c.Encrypted)
	s.oauthMu.RUnlock()
	return c, secret, e == nil
}
func (s *Server) memberPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	site, e := s.googleSite(r) // Uses only the managed exact-origin directory, independent of Google enablement.
	if strings.TrimSuffix(r.URL.Path, "/") == "/api/v1/auth/password-options" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		c, _, enabled := s.recoveryMail(r)
		writeJSON(w, 200, map[string]any{"data": map[string]any{"email_enabled": enabled && e == nil, "support_email": c.SupportEmail}})
		return
	}
	if e != nil {
		memberError(w, 403, "reset_unavailable", "Password recovery unavailable on this origin")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if strings.TrimSuffix(r.URL.Path, "/") == "/api/v1/auth/reset-password" {
		var in struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if memberDecode(r, &in) != nil {
			memberResult(w, nil, member.ErrInvalid)
			return
		}
		if !s.memberAttempt(w, r, "reset-password", "", 10) {
			return
		}
		e := (member.Service{DB: s.database}).ResetPassword(r.Context(), site.SiteID, in.Token, in.Password)
		if errors.Is(e, member.ErrResetToken) {
			memberError(w, 400, "invalid_reset_token", "Reset link is invalid or expired")
			return
		}
		if e == nil {
			http.SetCookie(w, userCookie("", -1, requestScheme(r) == "https"))
		}
		memberResult(w, map[string]bool{"reauthenticate": true}, e)
		return
	}
	var in struct {
		Email    string `json:"email"`
		Language string `json:"language"`
	}
	if memberDecode(r, &in) != nil || !membermail.Address(strings.TrimSpace(in.Email)) {
		memberResult(w, nil, member.ErrInvalid)
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !s.memberAttempt(w, r, "forgot-password", email, 5) {
		return
	}
	c, secret, enabled := s.recoveryMail(r)
	if !enabled {
		memberError(w, 503, "reset_unavailable", "Email recovery is unavailable")
		return
	}
	// Bounded asynchronous delivery keeps SMTP/account existence out of response timing.
	select {
	case s.resetJobs <- struct{}{}:
	default:
		memberError(w, 503, "unavailable", "Try again later")
		return
	}
	send := s.mailSend
	if send == nil {
		send = membermail.Send
	}
	origin := site.PublicURL
	siteID := site.SiteID
	language := in.Language
	go func() {
		defer func() { <-s.resetJobs }()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		svc := member.Service{DB: s.database}
		token, id, e := svc.IssuePasswordReset(ctx, siteID, email, c.Revision)
		if e != nil || token == "" {
			return
		}
		subject, body := membermail.ResetMessage(language, origin+"/shop/reset-password#token="+token)
		if e = send(ctx, c, secret, email, subject, body); e != nil {
			// Store no SMTP error text, recipient or token in diagnostics.
			s.database.ExecContext(ctx, `DELETE FROM gocms_member_password_reset WHERE token_hash=?`, member.TokenHash(token))
			s.database.ExecContext(ctx, `INSERT INTO gocms_user_event(user_id,action) VALUES(?,'password_reset_delivery_failed')`, id)
		}
	}()
	memberResult(w, map[string]bool{"accepted": true}, nil)
}
