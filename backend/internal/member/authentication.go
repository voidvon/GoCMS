package member

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"

	"gocms/internal/auth"
)

var ErrSessionLimit = errors.New("session limit reached")

var ErrConflict = errors.New("account conflict")

func TokenHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(v[:])
}
func (s Service) Register(ctx context.Context, username, email, password string, siteID ...int64) (Profile, error) {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "@ \t\n\r") || len(password) < 8 || len(password) > 1024 || len(email) > 254 {
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
	result, err := s.DB.ExecContext(ctx, "INSERT INTO gocms_user(site_id,username,email,password_hash,display_name) VALUES(?,?,?,?,?)", sid, username, email, encoded, username)
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
	_, _ = s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO gocms_site_member(site_id,user_id,display_name,status) VALUES(?,?,?,?)", sid, id, username, "active")
	return s.Profile(ctx, id)
}
func (s Service) Login(ctx context.Context, identifier, password, ip, agent string, currentToken string) (Profile, string, error) {
	if len(identifier) > 254 || len(password) > 1024 {
		return Profile{}, "", ErrInvalid
	}
	var id int64
	var encoded, status string
	err := s.DB.QueryRowContext(ctx, "SELECT id,password_hash,status FROM gocms_user WHERE username=? OR (email<>'' AND email=?)", identifier, strings.ToLower(identifier)).Scan(&id, &encoded, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Profile{}, "", err
	}
	if err != nil || status != "active" || !auth.ComparePassword(password, encoded) {
		if _, e := s.DB.ExecContext(ctx, "INSERT INTO gocms_user_login(identifier,success,ip) VALUES(?,0,?)", identifier, ip); e != nil {
			return Profile{}, "", e
		}
		return Profile{}, "", ErrUnauthorized
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return Profile{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, "", err
	}
	defer tx.Rollback()

	// Acquire the SQLite write lock before counting to serialize concurrent logins.
	if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return Profile{}, "", err
	}
	var limit int
	if err = tx.QueryRowContext(ctx, "SELECT max_sessions FROM gocms_user WHERE id=? AND status='active' AND password_hash=?", id, encoded).Scan(&limit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, "", ErrUnauthorized
		}
		return Profile{}, "", err
	}
	// Only the supplied live session belonging to this account can be replaced.
	if currentToken != "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE user_id=? AND token_hash=?", id, TokenHash(currentToken)); err != nil {
			return Profile{}, "", err
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM gocms_user_session WHERE user_id=?", id).Scan(&count); err != nil {
		return Profile{}, "", err
	}
	if count >= limit {
		return Profile{}, "", ErrSessionLimit
	}
	// Recheck the hash/status so concurrent disable or password change cannot create
	// a session after revocation.
	result, err := tx.ExecContext(ctx, "INSERT INTO gocms_user_session(token_hash,user_id,expires_at,ip,user_agent) SELECT ?,id,?,?,? FROM gocms_user WHERE id=? AND status='active' AND password_hash=?", TokenHash(token), time.Now().Add(30*24*time.Hour).Unix(), ip, agent, id, encoded)
	if err != nil {
		return Profile{}, "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Profile{}, "", err
	}
	if n != 1 {
		return Profile{}, "", ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return Profile{}, "", err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE gocms_user SET last_login_at=CURRENT_TIMESTAMP,last_login_ip=? WHERE id=?", ip, id); err != nil {
		return Profile{}, "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gocms_user_login(user_id,identifier,success,ip) VALUES(?,?,1,?)", id, identifier, ip); err != nil {
		return Profile{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Profile{}, "", err
	}
	profile, err := s.Profile(ctx, id)
	return profile, token, err
}

func (s Service) Authenticate(ctx context.Context, token string) (Profile, error) {
	var p Profile
	err := s.DB.QueryRowContext(ctx, "SELECT u.id,u.username,u.email,u.display_name,u.avatar_url FROM gocms_user u JOIN gocms_user_session s ON s.user_id=u.id WHERE s.token_hash=? AND s.expires_at>? AND u.status='active'", TokenHash(token), time.Now().Unix()).Scan(&p.ID, &p.Username, &p.Email, &p.DisplayName, &p.AvatarURL)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrUnauthorized
	}
	return p, err
}
func (s Service) Logout(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE token_hash=?", TokenHash(token))
	return err
}
