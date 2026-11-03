package site

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

const (
	adminSessionIdleTimeout     = 24 * time.Hour
	adminSessionAbsoluteTimeout = 7 * 24 * time.Hour
	adminSessionRenewInterval   = 5 * time.Minute
)

func ensureAdminSessions(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS gocms_admin_session (
		token TEXT PRIMARY KEY, username TEXT NOT NULL, expires_at INTEGER NOT NULL,
		absolute_expires_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return fmt.Errorf("create admin session table: %w", err)
	}
	var exists int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('gocms_admin_session') WHERE name='absolute_expires_at'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if _, err = tx.Exec(`ALTER TABLE gocms_admin_session ADD COLUMN absolute_expires_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("migrate admin sessions: %w", err)
		}
		// Legacy expiry was exactly login + 24h. Preserve its original login time.
		if _, err = tx.Exec(`UPDATE gocms_admin_session SET absolute_expires_at=expires_at+?`, int64((adminSessionAbsoluteTimeout-adminSessionIdleTimeout)/time.Second)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_gocms_admin_session_expiry ON gocms_admin_session(expires_at)`); err != nil {
		return err
	}
	return tx.Commit()
}

// Called only after route authorization, before the handler writes its response.
// Background polling authenticates normally but must not keep an idle tab alive.
func (s *Server) renewAdminSession(w http.ResponseWriter, r *http.Request) {
	if s.database == nil || s.resolveApiKeyToken(r) != "" || r.Header.Get("X-GoCMS-Background") == "1" {
		return
	}
	cookie, err := r.Cookie("gocms_admin")
	if err != nil || cookie.Value == "" {
		return
	}
	now := time.Now().Unix()
	var expiresAt, absoluteExpiresAt int64
	if err := s.database.QueryRowContext(r.Context(), `SELECT expires_at,absolute_expires_at FROM gocms_admin_session WHERE token=?`, cookie.Value).Scan(&expiresAt, &absoluteExpiresAt); err != nil {
		return
	}
	next := min(now+int64(adminSessionIdleTimeout/time.Second), absoluteExpiresAt)
	if expiresAt <= now || absoluteExpiresAt <= now || next-expiresAt < int64(adminSessionRenewInterval/time.Second) {
		return
	}
	// Conditional update never resurrects a revoked/expired session or shortens
	// an expiry already extended by a concurrent request.
	result, err := s.database.ExecContext(r.Context(), `UPDATE gocms_admin_session SET expires_at=?
		WHERE token=? AND expires_at>? AND absolute_expires_at>? AND expires_at<?`, next, cookie.Value, now, now, next)
	if err != nil {
		return
	}
	if rows, err := result.RowsAffected(); err == nil && rows > 0 {
		http.SetCookie(w, sessionCookie(cookie.Value, int(next-now)))
	}
}
