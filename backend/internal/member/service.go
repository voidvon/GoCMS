// Package member owns frontend account operations independently of HTTP and themes.
package member

import (
	"context"
	"database/sql"
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

// Both DB and Tx implement these reads, so login response preparation can share
// the transaction that commits the session and site membership.
type memberReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type Profile struct {
	HasPassword bool   `json:"has_password"`
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
	return loadProfile(ctx, s.DB, id)
}

func loadProfile(ctx context.Context, reader memberReader, id int64) (Profile, error) {
	var p Profile
	query := "SELECT id,username,email,display_name,avatar_url,password_hash<>'' FROM gocms_user WHERE id=? AND status='active'"
	err := reader.QueryRowContext(ctx, query, id).Scan(&p.ID, &p.Username, &p.Email, &p.DisplayName, &p.AvatarURL, &p.HasPassword)
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
func (s Service) Memberships(ctx context.Context, id int64, siteIDOpt ...int64) ([]Membership, error) {
	return loadMemberships(ctx, s.DB, id, siteIDOpt...)
}

func loadMemberships(ctx context.Context, reader memberReader, id int64, siteIDOpt ...int64) ([]Membership, error) {
	query := "SELECT g.id,g.name,g.slug,m.started_at,m.expires_at,m.status,g.status FROM gocms_user_group_member m JOIN gocms_user_group g ON g.id=m.group_id WHERE m.user_id=?"
	args := []any{id}
	if len(siteIDOpt) > 0 && siteIDOpt[0] > 0 {
		query += " AND g.site_id=?"
		args = append(args, siteIDOpt[0])
	}
	query += " ORDER BY g.sort_order,g.id"
	rows, err := reader.QueryContext(ctx, query, args...)
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
			return nil, errors.New("invalid stored membership start time")
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
				return nil, errors.New("invalid stored membership expiry time")
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

// ActiveGroupSlugs uses the same validity rules as the membership detail API.
func (s Service) ActiveGroupSlugs(ctx context.Context, id int64, siteID int64) ([]string, error) {
	memberships, err := s.Memberships(ctx, id, siteID)
	if err != nil {
		return nil, err
	}
	return activeGroupSlugs(memberships), nil
}

func activeGroupSlugs(memberships []Membership) []string {
	var groups []string
	for _, membership := range memberships {
		if membership.Status == "active" {
			groups = append(groups, membership.Slug)
		}
	}
	return groups
}
