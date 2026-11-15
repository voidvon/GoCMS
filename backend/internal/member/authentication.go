package member

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"gocms/internal/auth"
)

var ErrConflict = errors.New("account conflict")

var (
	usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)
	digitsPattern   = regexp.MustCompile(`^\d+$`)
)

// IsValidUsername validates a member username:
// - Length must be between 3 and 64 characters
// - Whitelist characters: a-z, A-Z, 0-9, underscores (_), hyphens (-)
// - Can start with a digit, but cannot be purely numeric
func IsValidUsername(username string) bool {
	return usernamePattern.MatchString(username) && !digitsPattern.MatchString(username)
}

func (s Service) Register(ctx context.Context, username, email, password string, siteID ...int64) (Profile, error) {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))
	if !IsValidUsername(username) || len(password) < 8 || len(password) > 1024 || len(email) > 254 {
		return Profile{}, ErrInvalid
	}
	if email != "" {
		a, e := mail.ParseAddress(email)
		if e != nil || a.Address != email {
			return Profile{}, ErrInvalid
		}
	}
	encoded, err := auth.HashPassword(password)
	if err != nil {
		return Profile{}, err
	}
	sid := int64(1)
	if len(siteID) > 0 && siteID[0] > 0 {
		sid = siteID[0]
	}
	siteIDsJSON := fmt.Sprintf("[%d]", sid)
	result, err := s.DB.ExecContext(ctx, "INSERT INTO gocms_user(site_id,site_ids,username,email,password_hash,display_name) VALUES(?,?,?,?,?,?)", sid, siteIDsJSON, username, email, encoded, username)
	if err != nil {
		var constraint interface{ Code() int }
		if errors.As(err, &constraint) && constraint.Code()&255 == 19 {
			return Profile{}, ErrConflict
		}
		return Profile{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Profile{}, err
	}
	var defaultGroupID int64
	if err := s.DB.QueryRowContext(ctx, "SELECT id FROM gocms_user_group WHERE site_id=? AND is_default=1 AND status='active' LIMIT 1", sid).Scan(&defaultGroupID); err == nil && defaultGroupID > 0 {
		_, _ = s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO gocms_user_group_member(user_id,group_id,status) VALUES(?,?,'active')", id, defaultGroupID)
	}
	return s.Profile(ctx, id)
}

type LoginInput struct {
	Identifier   string
	Password     string
	SiteID       int64
	IP           string
	UserAgent    string
	CurrentToken string
}

// LoginResult is an application result, never an HTTP response. In particular,
// transports must explicitly opt in to exposing Session.Token.
type LoginResult struct {
	User         Profile      `json:"-"`
	Session      SessionGrant `json:"-"`
	ActiveGroups []string     `json:"-"`
}

// Login verifies a password and issues a session independently of HTTP transport.
func (s Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	identifier, password := strings.TrimSpace(in.Identifier), in.Password
	ip, agent, currentToken := in.IP, in.UserAgent, in.CurrentToken
	if len(identifier) > 254 || len(password) > 1024 {
		return LoginResult{}, ErrInvalid
	}
	var id int64
	var encoded, status string
	query := "SELECT u.id, u.password_hash, u.status FROM gocms_user u WHERE (u.username=? OR (u.email<>'' AND u.email=?))"
	args := []any{identifier, strings.ToLower(identifier)}
	err := s.DB.QueryRowContext(ctx, query, args...).Scan(&id, &encoded, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, err
	}
	if err != nil || status != "active" || !auth.ComparePassword(password, encoded) {
		if _, e := s.DB.ExecContext(ctx, "INSERT INTO gocms_user_login(identifier,success,ip) VALUES(?,0,?)", identifier, ip); e != nil {
			return LoginResult{}, e
		}
		return LoginResult{}, ErrUnauthorized
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()
	session, err := createSession(ctx, tx, id, encoded, identifier, ip, agent, currentToken)
	if err != nil {
		return LoginResult{}, err
	}
	if in.SiteID > 0 {
		if err = joinMemberSite(ctx, tx, id, in.SiteID, false); err != nil {
			return LoginResult{}, err
		}
	}
	result, err := prepareLoginResult(ctx, tx, id, in.SiteID, session)
	if err != nil {
		return LoginResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

// Prepare public account data before commit: failure must roll back issuance or
// rotation instead of consuming a login slot without delivering its credential.
func prepareLoginResult(ctx context.Context, reader memberReader, id, siteID int64, session SessionGrant) (LoginResult, error) {
	profile, err := loadProfile(ctx, reader, id)
	if err != nil {
		return LoginResult{}, err
	}
	memberships, err := loadMemberships(ctx, reader, id, siteID)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: profile, Session: session, ActiveGroups: activeGroupSlugs(memberships)}, nil
}
