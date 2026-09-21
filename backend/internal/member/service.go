// Package member owns frontend account operations independently of HTTP and themes.
package member

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"gocms/internal/auth"
)

var ErrInvalid = errors.New("invalid input")
var ErrUnauthorized = errors.New("authentication required")
var ErrNotFound = errors.New("not found")

type Service struct{ DB *sql.DB }
type Profile struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}
type Membership struct {
	GroupID   int64   `json:"group_id"`
	Name      string  `json:"name"`
	Slug      string  `json:"slug"`
	StartsAt  string  `json:"started_at"`
	ExpiresAt *string `json:"expires_at"`
	Status    string  `json:"status"`
}

func ParseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalid
}
func (s Service) Profile(ctx context.Context, id int64) (Profile, error) {
	var p Profile
	err := s.DB.QueryRowContext(ctx, "SELECT id,username,email,display_name,avatar_url FROM gocms_user WHERE id=? AND status='active'", id).Scan(&p.ID, &p.Username, &p.Email, &p.DisplayName, &p.AvatarURL)
	return p, err
}
func (s Service) UpdateProfile(ctx context.Context, id int64, name, avatar *string) error {
	if name == nil && avatar == nil {
		return ErrInvalid
	}
	if name != nil && (len(strings.TrimSpace(*name)) == 0 || len(*name) > 200) {
		return ErrInvalid
	}
	if avatar != nil && *avatar != "" {
		u, e := url.Parse(*avatar)
		if e != nil || len(*avatar) > 2048 || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return ErrInvalid
		}
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE gocms_user SET display_name=COALESCE(?,display_name),avatar_url=COALESCE(?,avatar_url),updated_at=CURRENT_TIMESTAMP WHERE id=?", name, avatar, id)
	return err
}
func (s Service) ChangePassword(ctx context.Context, id int64, old, next string) error {
	if len(old) > 1024 || len(next) < 8 || len(next) > 1024 {
		return ErrInvalid
	}
	var hash string
	if err := s.DB.QueryRowContext(ctx, "SELECT password_hash FROM gocms_user WHERE id=? AND status='active'", id).Scan(&hash); err != nil {
		return err
	}
	if !auth.ComparePassword(old, hash) {
		return ErrUnauthorized
	}
	encoded, err := auth.HashPassword(next)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE gocms_user SET password_hash=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND password_hash=? AND status='active'", encoded, id, hash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE user_id=?", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gocms_user_event(user_id,action) VALUES(?,'password_changed')", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Service) Memberships(ctx context.Context, id int64) ([]Membership, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT g.id,g.name,g.slug,m.started_at,m.expires_at,m.status,g.status FROM gocms_user_group_member m JOIN gocms_user_group g ON g.id=m.group_id WHERE m.user_id=? ORDER BY g.sort_order,g.id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	now := time.Now()
	for rows.Next() {
		var m Membership
		var gs string
		if err := rows.Scan(&m.GroupID, &m.Name, &m.Slug, &m.StartsAt, &m.ExpiresAt, &m.Status, &gs); err != nil {
			return nil, err
		}
		start, e := ParseTime(m.StartsAt)
		if e != nil {
			return nil, e
		}
		m.StartsAt = start.Format(time.RFC3339)
		if m.Status != "active" || gs != "active" {
			m.Status = "disabled"
		} else if start.After(now) {
			m.Status = "scheduled"
		}
		if m.ExpiresAt != nil && *m.ExpiresAt != "" {
			end, e := ParseTime(*m.ExpiresAt)
			if e != nil {
				return nil, e
			}
			v := end.Format(time.RFC3339)
			m.ExpiresAt = &v
			if !end.After(now) && m.Status != "disabled" {
				m.Status = "expired"
			}
		} else {
			m.ExpiresAt = nil
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type Session struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Current   bool   `json:"current"`
}

// SessionID is a non-authenticating public reference; token hashes are never exposed.
func SessionID(hash string) string {
	v := sha256.Sum256([]byte("session-id:" + hash))
	return hex.EncodeToString(v[:])
}
func (s Service) Sessions(ctx context.Context, id int64, current string) ([]Session, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT token_hash,expires_at,created_at,ip,user_agent FROM gocms_user_session WHERE user_id=? AND expires_at>? ORDER BY created_at DESC", id, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var v Session
		var hash, created string
		var exp int64
		if err := rows.Scan(&hash, &exp, &created, &v.IP, &v.UserAgent); err != nil {
			return nil, err
		}
		t, err := ParseTime(created)
		if err != nil {
			return nil, err
		}
		v.ID = SessionID(hash)
		v.Current = hash == current
		v.ExpiresAt = time.Unix(exp, 0).UTC().Format(time.RFC3339)
		v.CreatedAt = t.Format(time.RFC3339)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s Service) Revoke(ctx context.Context, id int64, reference string) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT token_hash FROM gocms_user_session WHERE user_id=?", id)
	if err != nil {
		return err
	}
	found := ""
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return err
		}
		if SessionID(hash) == reference {
			found = hash
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if found == "" {
		return ErrNotFound
	}
	_, err = s.DB.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE user_id=? AND token_hash=?", id, found)
	return err
}

// Admit reserves an attempt before expensive password work, including successful
// attempts, so concurrency cannot bypass the limit. Key is a hashed IP/account.
func (s Service) Admit(ctx context.Context, key string, limit int) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM gocms_user_event WHERE action=? AND created_at>=datetime('now','-15 minutes')", key).Scan(&n); err != nil {
		return false, err
	}
	if n >= limit {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gocms_user_event(action) VALUES(?)", key); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
