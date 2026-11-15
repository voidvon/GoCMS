package site

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gocms/internal/member"
)

const memberCookieName = "gocms_user"

type memberTransport uint8

const (
	memberCookieTransport memberTransport = iota
	memberBearerTransport
)

// memberCredential is private to HTTP. The member service never receives a
// Cookie, Authorization header, or transport flag.
type memberCredential struct {
	token     string
	transport memberTransport
	present   bool
}

func cookieMemberCredential(r *http.Request) memberCredential {
	credential := memberCredential{transport: memberCookieTransport}
	if cookie, err := r.Cookie(memberCookieName); err == nil {
		credential.token, credential.present = cookie.Value, true
	}
	return credential
}

func bearerMemberCredential(r *http.Request) (memberCredential, error) {
	credential := memberCredential{transport: memberBearerTransport}
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return credential, nil
	}
	credential.present = true
	if len(values) != 1 {
		return credential, member.ErrUnauthorized
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || len(fields[1]) > 512 {
		return credential, member.ErrUnauthorized
	}
	credential.token = fields[1]
	return credential, nil
}

// An explicit Authorization header selects the identity for the whole request.
// A malformed or invalid credential must never fall back to another cookie user.
func requestMemberCredential(r *http.Request) (memberCredential, error) {
	if len(r.Header.Values("Authorization")) != 0 {
		return bearerMemberCredential(r)
	}
	return cookieMemberCredential(r), nil
}

func (transport memberTransport) loginCredential(r *http.Request) (memberCredential, error) {
	if transport == memberBearerTransport {
		return bearerMemberCredential(r)
	}
	// The browser login endpoint only rotates a browser's existing session.
	return cookieMemberCredential(r), nil
}

func (credential memberCredential) clearCookie(w http.ResponseWriter, r *http.Request) {
	if credential.transport == memberCookieTransport {
		http.SetCookie(w, userCookie("", -1, requestScheme(r) == "https"))
	}
}

type memberLoginResponse struct {
	User        siteUser `json:"user"`
	AccessToken string   `json:"access_token,omitempty"`
	TokenType   string   `json:"token_type,omitempty"`
	ExpiresIn   int      `json:"expires_in,omitempty"`
}

// Only the selected transport may deliver a newly issued credential. Expiration
// comes from the committed session grant, not a second policy in the HTTP layer.
func (transport memberTransport) deliverSession(w http.ResponseWriter, r *http.Request, user siteUser, grant member.SessionGrant) memberLoginResponse {
	response := memberLoginResponse{User: user}
	if transport == memberBearerTransport {
		response.AccessToken, response.TokenType, response.ExpiresIn = grant.Token, "Bearer", memberSessionSeconds(grant)
	} else {
		setMemberSessionCookie(w, r, grant)
	}
	return response
}

func memberSessionSeconds(grant member.SessionGrant) int {
	return max(0, int(time.Until(grant.ExpiresAt)/time.Second))
}

func setMemberSessionCookie(w http.ResponseWriter, r *http.Request, grant member.SessionGrant) {
	cookie := userCookie(grant.Token, memberSessionSeconds(grant), requestScheme(r) == "https")
	cookie.Expires = grant.ExpiresAt
	http.SetCookie(w, cookie)
}

type memberAuthentication struct {
	identity   member.Identity
	credential memberCredential
	siteID     int64
}

// Every protected member API authenticates once and passes this result to its
// handler. Authorization, session references and cookie effects share that choice.
func (s *Server) authenticateMember(r *http.Request) (memberAuthentication, error) {
	credential, err := requestMemberCredential(r)
	authentication := memberAuthentication{credential: credential}
	if err != nil {
		return authentication, err
	}
	if credential.token == "" {
		return authentication, member.ErrUnauthorized
	}
	authentication.siteID, err = s.resolvePublicSiteID(r)
	if err != nil {
		return authentication, err
	}
	authentication.identity, err = (member.Service{DB: s.database}).Authenticate(r.Context(), credential.token, authentication.siteID)
	return authentication, err
}

func memberAuthenticationError(w http.ResponseWriter, credential memberCredential, err error) {
	if !errors.Is(err, member.ErrUnauthorized) {
		memberResult(w, nil, err)
		return
	}
	challenge := `Bearer realm="members"`
	if credential.transport == memberBearerTransport && credential.present {
		challenge += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	memberError(w, 401, "authentication_required", "Sign in required")
}

func userCookie(v string, age int, secure bool) *http.Cookie {
	return &http.Cookie{Name: memberCookieName, Value: v, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age, Secure: secure}
}
