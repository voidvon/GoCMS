package member

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gocms/internal/auth"
	"gocms/internal/memberoauth"
)

var ErrResetToken = errors.New("invalid or expired password reset token")

func EnsurePasswordRecovery(ctx context.Context, db *sql.DB) error {
	_, e := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_member_password_reset(token_hash TEXT PRIMARY KEY,user_id INTEGER NOT NULL UNIQUE,site_id INTEGER NOT NULL,email TEXT NOT NULL,password_fingerprint TEXT NOT NULL,expires_at INTEGER NOT NULL,channel TEXT NOT NULL,revision TEXT NOT NULL DEFAULT '',issuer TEXT NOT NULL DEFAULT '',subject TEXT NOT NULL DEFAULT '',FOREIGN KEY(user_id) REFERENCES gocms_user(id) ON DELETE CASCADE,FOREIGN KEY(site_id) REFERENCES gocms_site(id) ON DELETE CASCADE)`)
	return e
}
func resetGrant(ctx context.Context, tx *sql.Tx, id, site int64, email, hash, channel, revision, issuer, subject string) (string, error) {
	token, e := memberoauth.Random()
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO gocms_member_password_reset(token_hash,user_id,site_id,email,password_fingerprint,expires_at,channel,revision,issuer,subject) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET token_hash=excluded.token_hash,site_id=excluded.site_id,email=excluded.email,password_fingerprint=excluded.password_fingerprint,expires_at=excluded.expires_at,channel=excluded.channel,revision=excluded.revision,issuer=excluded.issuer,subject=excluded.subject`, TokenHash(token), id, site, email, TokenHash(hash), time.Now().Add(30*time.Minute).Unix(), channel, revision, issuer, subject)
	return token, e
}

// IssuePasswordReset never exposes account existence through the public API.
func (s Service) IssuePasswordReset(ctx context.Context, site int64, email string, revision string) (string, int64, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", 0, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM gocms_member_password_reset WHERE expires_at<=?`, time.Now().Unix()); e != nil {
		return "", 0, e
	}
	var activeRevision string
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM gocms_platform_mail_config WHERE id=1 AND json_extract(config,'$.enabled')=1 AND revision=?`, revision).Scan(&activeRevision); e != nil {
		return "", 0, ErrUnauthorized
	}
	var id int64
	var hash, storedEmail string
	e = tx.QueryRowContext(ctx, `SELECT id,password_hash,email FROM gocms_user WHERE lower(email)=? AND email<>'' AND status='active' AND (site_id=? OR EXISTS(SELECT 1 FROM json_each(site_ids) WHERE value=?))`, strings.ToLower(strings.TrimSpace(email)), site, site).Scan(&id, &hash, &storedEmail)
	if errors.Is(e, sql.ErrNoRows) {
		return "", 0, nil
	}
	if e != nil {
		return "", 0, e
	}
	token, e := resetGrant(ctx, tx, id, site, storedEmail, hash, "email", revision, "", "")
	if e != nil {
		return "", 0, e
	}
	return token, id, tx.Commit()
}

// External recovery accepts only an existing verified binding. It cannot register,
// merge by email, join another site or create an authenticated member session.
func (s Service) IssueExternalPasswordReset(ctx context.Context, site int64, provider, revision, subject string) (string, error) {
	if (provider != "google" && provider != "facebook") || subject == "" || len(subject) > 255 {
		return "", ErrUnauthorized
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM gocms_member_password_reset WHERE expires_at<=?`, time.Now().Unix()); e != nil {
		return "", e
	}
	var clientID string
	e = tx.QueryRowContext(ctx, `SELECT c.client_id FROM gocms_platform_oauth_config c JOIN gocms_site s ON s.id=? WHERE c.provider=? AND c.enabled=1 AND c.revision=? AND s.status='active'`, site, provider, revision).Scan(&clientID)
	if e != nil {
		return "", ErrUnauthorized
	}
	issuer := memberoauth.Config{Provider: provider}.Issuer()
	if provider == "facebook" {
		issuer += "/" + clientID
	}
	id, e := resolveProviderIdentity(ctx, tx, provider, issuer, subject)
	if e != nil || id == 0 {
		return "", ErrUnauthorized
	}
	var email, hash string
	e = tx.QueryRowContext(ctx, `SELECT email,password_hash FROM gocms_user WHERE id=? AND status='active' AND (site_id=? OR EXISTS(SELECT 1 FROM json_each(site_ids) WHERE value=?))`, id, site, site).Scan(&email, &hash)
	if e != nil {
		return "", ErrUnauthorized
	}
	token, e := resetGrant(ctx, tx, id, site, email, hash, provider, revision, issuer, subject)
	if e != nil {
		return "", e
	}
	return token, tx.Commit()
}

const resetProofSQL = ` AND ((r.channel='email' AND EXISTS(SELECT 1 FROM gocms_platform_mail_config WHERE id=1 AND json_extract(config,'$.enabled')=1 AND revision=r.revision)) OR (r.channel IN ('google','facebook') AND EXISTS(SELECT 1 FROM gocms_platform_oauth_config c JOIN gocms_platform_identity i ON i.provider=c.provider WHERE c.provider=r.channel AND c.enabled=1 AND c.revision=r.revision AND i.user_id=r.user_id AND i.issuer=r.issuer AND i.subject=r.subject)))`

func (s Service) ResetPassword(ctx context.Context, site int64, token, next string) error {
	if len(token) != 43 {
		return ErrResetToken
	}
	if len(next) < 8 || len(next) > 1024 {
		return ErrInvalid
	}
	var id int64
	var email, fingerprint, hash string
	e := s.DB.QueryRowContext(ctx, `SELECT r.user_id,r.email,r.password_fingerprint,u.password_hash FROM gocms_member_password_reset r JOIN gocms_user u ON u.id=r.user_id JOIN gocms_site s ON s.id=r.site_id WHERE r.token_hash=? AND r.site_id=? AND r.expires_at>? AND u.status='active' AND s.status='active' AND u.email=r.email AND (u.site_id=? OR EXISTS(SELECT 1 FROM json_each(u.site_ids) WHERE value=?))`+resetProofSQL, TokenHash(token), site, time.Now().Unix(), site, site).Scan(&id, &email, &fingerprint, &hash)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrResetToken
	}
	if e != nil {
		return e
	}
	if TokenHash(hash) != fingerprint {
		return ErrResetToken
	}
	encoded, e := auth.HashPassword(next)
	if e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Recheck channel configuration and binding inside the same write transaction.
	result, e := tx.ExecContext(ctx, `DELETE FROM gocms_member_password_reset AS r WHERE token_hash=? AND user_id=? AND site_id=? AND expires_at>? AND email=? AND password_fingerprint=?`+resetProofSQL, TokenHash(token), id, site, time.Now().Unix(), email, fingerprint)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrResetToken
	}
	result, e = tx.ExecContext(ctx, `UPDATE gocms_user SET password_hash=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND password_hash=? AND email=? AND status='active' AND (site_id=? OR EXISTS(SELECT 1 FROM json_each(site_ids) WHERE value=?)) AND EXISTS(SELECT 1 FROM gocms_site WHERE id=? AND status='active')`, encoded, id, hash, email, site, site, site)
	if e != nil {
		return e
	}
	n, _ = result.RowsAffected()
	if n != 1 {
		return ErrResetToken
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM gocms_user_session WHERE user_id=?`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO gocms_user_event(user_id,action) VALUES(?,'password_reset')`, id); e != nil {
		return e
	}
	return tx.Commit()
}
