package site

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gocms/internal/member"
)

// Browser mutations require JSON and a same-origin request. No implicit CORS or
// forwarded-host trust: cross-origin deployments should use a same-origin proxy.
func memberRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		return true
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		memberError(w, 403, "origin_not_allowed", "Cross-site request rejected")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := requestScheme(r)
		expectedHost := requestHost(r)
		if err != nil || !strings.EqualFold(u.Host, expectedHost) || !strings.EqualFold(u.Scheme, scheme) {
			memberError(w, 403, "origin_not_allowed", "Origin rejected")
			return false
		}
	}
	if r.Method != http.MethodDelete && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		memberError(w, 415, "unsupported_media_type", "Use application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	return true
}
func memberError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func memberDecode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return member.ErrInvalid
	}
	return nil
}
func memberResult(w http.ResponseWriter, data any, err error) {
	if err == nil {
		writeJSON(w, 200, map[string]any{"data": data})
		return
	}
	switch {
	case errors.Is(err, member.ErrSessionLimit):
		memberError(w, 409, "session_limit_reached", "登录会话已达上限，请退出其他会话或联系管理员")
	case errors.Is(err, member.ErrConflict):
		memberError(w, 409, "account_conflict", "Account already exists")
	case errors.Is(err, member.ErrInvalid):
		memberError(w, 400, "invalid_input", "Invalid input")
	case errors.Is(err, member.ErrUnauthorized):
		memberError(w, 401, "invalid_credentials", "Invalid credentials")
	case errors.Is(err, member.ErrNotFound):
		memberError(w, 404, "not_found", "Resource not found")
	default:
		memberError(w, 500, "internal_error", "Unable to complete request")
	}
}
func (s *Server) memberAPI(w http.ResponseWriter, r *http.Request) {
	if s.database == nil {
		memberError(w, 503, "unavailable", "Database unavailable")
		return
	}
	if !memberRequest(w, r) {
		return
	}
	p := strings.TrimSuffix(r.URL.Path, "/")
	// Authentication endpoints retain their existing response during migration.
	switch p {
	case "/api/v1/auth/register":
		s.memberAuthAdapter(w, r, s.userRegister)
		return
	case "/api/v1/auth/login":
		s.memberAuthAdapter(w, r, s.userLogin)
		return
	case "/api/v1/auth/logout":
		s.memberAuthAdapter(w, r, s.userLogout)
		return
	}
	u, ok := s.currentSiteUser(r)
	if !ok {
		memberError(w, 401, "authentication_required", "Sign in required")
		return
	}
	service := member.Service{DB: s.database}
	cookie, _ := r.Cookie("gocms_user")
	switch {
	case p == "/api/v1/me" && r.Method == http.MethodGet:
		data, err := service.Profile(r.Context(), u.ID)
		memberResult(w, data, err)
	case p == "/api/v1/me" && r.Method == http.MethodPatch:
		var in struct {
			DisplayName *string `json:"display_name"`
			AvatarURL   *string `json:"avatar_url"`
		}
		if memberDecode(r, &in) != nil {
			memberResult(w, nil, member.ErrInvalid)
			return
		}
		if err := service.UpdateProfile(r.Context(), u.ID, in.DisplayName, in.AvatarURL); err != nil {
			memberResult(w, nil, err)
			return
		}
		data, err := service.Profile(r.Context(), u.ID)
		memberResult(w, data, err)
	case p == "/api/v1/me/password" && r.Method == http.MethodPut:
		var in struct {
			Old string `json:"old_password"`
			New string `json:"new_password"`
		}
		if memberDecode(r, &in) != nil {
			memberResult(w, nil, member.ErrInvalid)
			return
		}
		if !s.memberAttempt(w, r, "password", u.Username, 10) {
			return
		}
		err := service.ChangePassword(r.Context(), u.ID, in.Old, in.New)
		if err == nil {
			http.SetCookie(w, userCookie("", -1, requestScheme(r) == "https"))
		}
		memberResult(w, map[string]bool{"reauthenticate": true}, err)
	case p == "/api/v1/me/memberships" && r.Method == http.MethodGet:
		siteID, _ := s.resolveSiteID(r, nil)
		data, err := service.Memberships(r.Context(), u.ID, siteID)
		memberResult(w, data, err)
	case p == "/api/v1/me/sessions" && r.Method == http.MethodGet:
		data, err := service.Sessions(r.Context(), u.ID, hashToken(cookie.Value))
		memberResult(w, data, err)
	case strings.HasPrefix(p, "/api/v1/me/sessions/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(p, "/api/v1/me/sessions/")
		err := service.Revoke(r.Context(), u.ID, id)
		if err == nil && id == member.SessionID(hashToken(cookie.Value)) {
			http.SetCookie(w, userCookie("", -1, requestScheme(r) == "https"))
		}
		memberResult(w, map[string]bool{"ok": true}, err)
	default:
		memberError(w, 405, "method_not_allowed", "Unsupported endpoint or method")
	}
}

// Adapter preserves legacy authentication payloads while v1 uses one envelope.
func (s *Server) memberAuthAdapter(w http.ResponseWriter, r *http.Request, handler http.HandlerFunc) {
	recorder := &memberResponse{header: make(http.Header), Code: 200}
	handler(recorder, r)
	if retry := recorder.header.Get("Retry-After"); retry != "" {
		w.Header().Set("Retry-After", retry)
	}
	for _, cookie := range (&http.Response{Header: recorder.header}).Cookies() {
		http.SetCookie(w, cookie)
	}
	if recorder.Code >= 400 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(recorder.Body.Bytes(), &payload) == nil && payload.Error.Code != "" {
			memberError(w, recorder.Code, payload.Error.Code, payload.Error.Message)
			return
		}

		code := "invalid_input"
		switch recorder.Code {
		case 401:
			code = "invalid_credentials"
		case 409:
			code = "account_conflict"
		case 429:
			code = "rate_limited"
		case 405:
			code = "method_not_allowed"
		case 500:
			code = "internal_error"
		}
		memberError(w, recorder.Code, code, http.StatusText(recorder.Code))
		return
	}
	var data any
	if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
		memberResult(w, nil, err)
		return
	}
	writeJSON(w, recorder.Code, map[string]any{"data": data})
}
func (s *Server) memberAttempt(w http.ResponseWriter, r *http.Request, action, identifier string, limit int) bool {
	for _, key := range []string{"ip:" + clientIP(r), "account:" + strings.ToLower(identifier)} {
		allowed, err := (member.Service{DB: s.database}).Admit(r.Context(), "rate:"+action+":"+hashToken(key), limit)
		if err != nil {
			memberResult(w, nil, err)
			return false
		}
		if !allowed {
			w.Header().Set("Retry-After", "900")
			memberError(w, 429, "rate_limited", "Try again later")
			return false
		}
	}
	return true
}

type memberResponse struct {
	header http.Header
	Body   bytes.Buffer
	Code   int
}

func (w *memberResponse) Header() http.Header         { return w.header }
func (w *memberResponse) WriteHeader(code int)        { w.Code = code }
func (w *memberResponse) Write(b []byte) (int, error) { return w.Body.Write(b) }
