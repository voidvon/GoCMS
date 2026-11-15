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

type memberResponseFormat uint8

const (
	memberLegacyResponse memberResponseFormat = iota
	memberV1Response
)

// Legacy compatibility ends at response encoding. v1 handlers invoke the same
// application operations directly, without capturing and reparsing HTTP output.
func writeMemberAuthResponse(w http.ResponseWriter, status int, format memberResponseFormat, data any) {
	if format == memberV1Response {
		data = map[string]any{"data": data}
	}
	writeJSON(w, status, data)
}

func memberAuthMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	memberError(w, 405, "method_not_allowed", "Unsupported method")
	return false
}

func (s *Server) userRegister(w http.ResponseWriter, r *http.Request) {
	s.memberRegister(w, r, memberLegacyResponse)
}

func (s *Server) memberRegister(w http.ResponseWriter, r *http.Request, format memberResponseFormat) {
	if !memberAuthMethod(w, r, http.MethodPost) {
		return
	}
	var in siteUserCredentials
	if memberDecode(r, &in) != nil {
		memberResult(w, nil, member.ErrInvalid)
		return
	}
	if !s.memberAttempt(w, r, "register", in.Username, 10) {
		return
	}
	siteID, err := s.resolvePublicSiteID(r)
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	profile, err := (member.Service{DB: s.database}).Register(r.Context(), in.Username, in.Email, in.Password, siteID)
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	user, err := s.memberUser(r, profile, siteID)
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	writeMemberAuthResponse(w, http.StatusCreated, format, map[string]any{"user": user})
}

func (s *Server) userLogin(w http.ResponseWriter, r *http.Request) {
	s.memberLogin(w, r, memberCookieTransport, memberLegacyResponse)
}

func (s *Server) memberLogin(w http.ResponseWriter, r *http.Request, transport memberTransport, format memberResponseFormat) {
	if !memberAuthMethod(w, r, http.MethodPost) {
		return
	}
	var in siteUserCredentials
	if memberDecode(r, &in) != nil {
		memberResult(w, nil, member.ErrInvalid)
		return
	}
	identifier := strings.TrimSpace(in.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(in.Username)
	}
	if !s.memberAttempt(w, r, "login", identifier, 20) {
		return
	}
	credential, err := transport.loginCredential(r)
	if err != nil {
		memberAuthenticationError(w, credential, err)
		return
	}
	siteID, err := s.resolvePublicSiteID(r)
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	result, err := (member.Service{DB: s.database}).Login(r.Context(), member.LoginInput{
		Identifier: identifier, Password: in.Password, SiteID: siteID,
		IP: clientIP(r), UserAgent: r.UserAgent(), CurrentToken: credential.token,
	})
	if err != nil {
		memberResult(w, nil, err)
		return
	}
	user := memberUserResponse(result.User, result.ActiveGroups)
	response := transport.deliverSession(w, r, user, result.Session)
	writeMemberAuthResponse(w, http.StatusOK, format, response)
}

func (s *Server) userSession(w http.ResponseWriter, r *http.Request) {
	if !memberAuthMethod(w, r, http.MethodGet) {
		return
	}
	user, ok := s.currentSiteUser(r)
	if !ok {
		writeJSON(w, 200, map[string]any{"user": nil})
		return
	}
	writeJSON(w, 200, map[string]any{"user": user})
}

// Group validity belongs to the member service. Authentication responses and
// membership endpoints must agree about expired, scheduled and disabled groups.
func (s *Server) memberUser(r *http.Request, profile member.Profile, siteID int64) (siteUser, error) {
	groups, err := (member.Service{DB: s.database}).ActiveGroupSlugs(r.Context(), profile.ID, siteID)
	if err != nil {
		return siteUser{}, err
	}
	return memberUserResponse(profile, groups), nil
}

func memberUserResponse(profile member.Profile, groups []string) siteUser {
	return siteUser{ID: profile.ID, Username: profile.Username, Email: profile.Email, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, Groups: groups}
}

func (s *Server) userLogout(w http.ResponseWriter, r *http.Request) {
	s.memberLogout(w, r, memberLegacyResponse)
}

func (s *Server) memberLogout(w http.ResponseWriter, r *http.Request, format memberResponseFormat) {
	if !memberAuthMethod(w, r, http.MethodPost) {
		return
	}
	credential, err := requestMemberCredential(r)
	if err != nil {
		memberAuthenticationError(w, credential, err)
		return
	}
	if credential.token != "" {
		if err := (member.Service{DB: s.database}).Logout(r.Context(), credential.token); err != nil {
			memberResult(w, nil, err)
			return
		}
	}
	credential.clearCookie(w, r)
	writeMemberAuthResponse(w, http.StatusOK, format, map[string]bool{"ok": true})
}

func (s *Server) currentSiteUser(r *http.Request) (siteUser, bool) {
	authentication, err := s.authenticateMember(r)
	if err != nil {
		return siteUser{}, false
	}
	user, err := s.memberUser(r, authentication.identity.Profile, authentication.siteID)
	return user, err == nil
}

func hashToken(v string) string { return member.TokenHash(v) }
