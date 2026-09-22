package site

import (
	"net/http"
	"strings"

	"gocms/internal/member"
)

type siteUserCredentials struct {
	Identifier string `json:"identifier"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}
type siteUser struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Email       string   `json:"email,omitempty"`
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	Groups      []string `json:"groups,omitempty"`
}

func (s *Server) userRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var in siteUserCredentials
	if memberDecode(r, &in) != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	if !s.memberAttempt(w, r, "register", in.Username, 10) {
		return
	}
	siteID, _ := s.resolveSiteID(r, nil)
	p, err := (member.Service{DB: s.database}).Register(r.Context(), in.Username, in.Email, in.Password, siteID)
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	u := siteUser{ID: p.ID, Username: p.Username, Email: p.Email, DisplayName: p.DisplayName, AvatarURL: p.AvatarURL}
	writeJSON(w, 201, map[string]any{"user": u})

}

func (s *Server) userLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var in siteUserCredentials
	if memberDecode(r, &in) != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	idf := strings.TrimSpace(in.Identifier)
	if idf == "" {
		idf = strings.TrimSpace(in.Username)
	}
	if !s.memberAttempt(w, r, "login", idf, 20) {
		return
	}
	currentToken := ""
	if cookie, err := r.Cookie("gocms_user"); err == nil {
		currentToken = cookie.Value
	}
	siteID, _ := s.resolveSiteID(r, nil)
	p, token, err := (member.Service{DB: s.database}).Login(r.Context(), idf, in.Password, clientIP(r), r.UserAgent(), currentToken, siteID)
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	u := siteUser{ID: p.ID, Username: p.Username, Email: p.Email, DisplayName: p.DisplayName, AvatarURL: p.AvatarURL}
	s.attachMemberGroups(r, &u)
	http.SetCookie(w, userCookie(token, 30*24*60*60, r.TLS != nil))
	writeJSON(w, 200, map[string]any{"user": u})

}

func (s *Server) userSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	u, ok := s.currentSiteUser(r)
	if !ok {
		writeJSON(w, 200, map[string]any{"user": nil})
		return
	}
	writeJSON(w, 200, map[string]any{"user": u})
}

func (s *Server) attachMemberGroups(r *http.Request, u *siteUser) {
	siteID, _ := s.resolveSiteID(r, nil)
	if s.database == nil {
		return
	}
	query := `SELECT g.slug FROM gocms_user_group_member m JOIN gocms_user_group g ON m.group_id=g.id WHERE m.user_id=? AND m.status='active' AND g.status='active'`
	args := []any{u.ID}
	if siteID > 0 {
		query += ` AND g.site_id=?`
		args = append(args, siteID)
	}
	rows, err := s.database.QueryContext(r.Context(), query, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err == nil {
			u.Groups = append(u.Groups, slug)
		}
	}
}

func (s *Server) userLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if c, e := r.Cookie("gocms_user"); e == nil {
		if err := (member.Service{DB: s.database}).Logout(r.Context(), c.Value); err != nil {
			memberResult(w, nil, err)
			return
		}
	}
	http.SetCookie(w, userCookie("", -1, r.TLS != nil))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) currentSiteUser(r *http.Request) (siteUser, bool) {
	c, e := r.Cookie("gocms_user")
	if e != nil {
		return siteUser{}, false
	}
	p, err := (member.Service{DB: s.database}).Authenticate(r.Context(), c.Value)
	if err != nil {
		return siteUser{}, false
	}
	siteID, _ := s.resolveSiteID(r, nil)
	if siteID > 0 && s.database != nil {
		var memberCount int
		_ = s.database.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_user WHERE id=? AND (site_id=? OR id IN (SELECT user_id FROM gocms_site_member WHERE site_id=?))`, p.ID, siteID, siteID).Scan(&memberCount)
		if memberCount == 0 {
			return siteUser{}, false
		}
	}
	u := siteUser{ID: p.ID, Username: p.Username, Email: p.Email, DisplayName: p.DisplayName, AvatarURL: p.AvatarURL}
	s.attachMemberGroups(r, &u)
	return u, true
}
func hashToken(v string) string { return member.TokenHash(v) }
func userCookie(v string, age int, secure bool) *http.Cookie {
	return &http.Cookie{Name: "gocms_user", Value: v, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age, Secure: secure}
}
