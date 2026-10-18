package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const AdminNotificationTable = "gocms_admin_notification"

// Admin notification levels drive the badge styling in the admin shell.
const (
	AdminNotificationLevelInfo    = "info"
	AdminNotificationLevelWarning = "warning"
	AdminNotificationLevelError   = "error"
)

// AdminNotification is one entry of the admin notification inbox. Entries are
// site-scoped, and repeated occurrences of the same kind collapse into the
// existing unread entry (occurrence count + latest detail) so that a broken
// integration cannot flood the inbox.
type AdminNotification struct {
	ID          int64  `json:"id"`
	SiteID      int64  `json:"site_id"`
	Kind        string `json:"kind"`
	Level       string `json:"level"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	LinkView    string `json:"link_view"`
	Occurrences int64  `json:"occurrences"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	ReadAt      string `json:"read_at"`
}

// EnsureAdminNotifications creates the notification inbox table.
func EnsureAdminNotifications(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+AdminNotificationTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"site_id" INTEGER NOT NULL DEFAULT 1,
			"kind" TEXT NOT NULL DEFAULT '',
			"level" TEXT NOT NULL DEFAULT 'info',
			"title" TEXT NOT NULL DEFAULT '',
			"detail" TEXT NOT NULL DEFAULT '',
			"link_view" TEXT NOT NULL DEFAULT '',
			"occurrences" INTEGER NOT NULL DEFAULT 1,
			"created_at" TEXT NOT NULL DEFAULT '',
			"updated_at" TEXT NOT NULL DEFAULT '',
			"read_at" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create admin notification table: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_gocms_admin_notification_site
		ON "`+AdminNotificationTable+`" ("site_id", "read_at", "id")`); err != nil {
		return fmt.Errorf("create admin notification index: %w", err)
	}
	return nil
}

// adminNotificationRetention bounds the inbox by dropping the oldest read
// entries once a site exceeds this many.
const adminNotificationRetention = 50

// RecordAdminNotification stores a notification, folding it into the existing
// unread entry of the same kind when there is one. The lookup and the write
// share one transaction so that two failures racing each other still produce a
// single entry.
func RecordAdminNotification(ctx context.Context, database *sql.DB, notification AdminNotification) error {
	if database == nil {
		return nil
	}
	if notification.SiteID <= 0 {
		notification.SiteID = 1
	}
	if strings.TrimSpace(notification.Level) == "" {
		notification.Level = AdminNotificationLevelInfo
	}
	now := time.Now().Format("2006-01-02 15:04:05")

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin admin notification: %w", err)
	}
	defer tx.Rollback()

	var existingID int64
	err = tx.QueryRowContext(ctx, `
		SELECT "id" FROM "`+AdminNotificationTable+`"
		WHERE "site_id" = ? AND "kind" = ? AND "read_at" = ''
		ORDER BY "id" DESC LIMIT 1`, notification.SiteID, notification.Kind).Scan(&existingID)
	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx, `
			UPDATE "`+AdminNotificationTable+`"
			SET "occurrences" = "occurrences" + 1, "level" = ?, "detail" = ?, "updated_at" = ?
			WHERE "id" = ?`,
			notification.Level, notification.Detail, now, existingID); err != nil {
			return fmt.Errorf("update admin notification: %w", err)
		}

	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO "`+AdminNotificationTable+`"
			("site_id", "kind", "level", "title", "detail", "link_view", "occurrences", "created_at", "updated_at", "read_at")
			VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, '')`,
			notification.SiteID, notification.Kind, notification.Level, notification.Title,
			notification.Detail, notification.LinkView, now, now); err != nil {
			return fmt.Errorf("insert admin notification: %w", err)
		}
		if err := pruneAdminNotifications(ctx, tx, notification.SiteID); err != nil {
			return err
		}

	default:
		return fmt.Errorf("lookup admin notification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin notification: %w", err)
	}
	return nil
}

type notificationExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func pruneAdminNotifications(ctx context.Context, exec notificationExecer, siteID int64) error {
	if _, err := exec.ExecContext(ctx, `
		DELETE FROM "`+AdminNotificationTable+`"
		WHERE "site_id" = ? AND "read_at" <> '' AND "id" NOT IN (
			SELECT "id" FROM "`+AdminNotificationTable+`"
			WHERE "site_id" = ? ORDER BY "id" DESC LIMIT ?
		)`, siteID, siteID, adminNotificationRetention); err != nil {
		return fmt.Errorf("prune admin notifications: %w", err)
	}
	return nil
}

// ListAdminNotifications returns the newest notifications of a site with the
// unread ones first, plus the total unread count.
func ListAdminNotifications(ctx context.Context, database *sql.DB, siteID int64, limit int) ([]AdminNotification, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := database.QueryContext(ctx, `
		SELECT "id", "site_id", "kind", "level", "title", "detail", "link_view", "occurrences",
		       "created_at", "updated_at", "read_at"
		FROM "`+AdminNotificationTable+`"
		WHERE "site_id" = ?
		ORDER BY CASE WHEN "read_at" = '' THEN 0 ELSE 1 END, "id" DESC
		LIMIT ?`, siteID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list admin notifications: %w", err)
	}
	defer rows.Close()

	items := make([]AdminNotification, 0, limit)
	for rows.Next() {
		var item AdminNotification
		if err := rows.Scan(&item.ID, &item.SiteID, &item.Kind, &item.Level, &item.Title, &item.Detail,
			&item.LinkView, &item.Occurrences, &item.CreatedAt, &item.UpdatedAt, &item.ReadAt); err != nil {
			return nil, 0, fmt.Errorf("scan admin notification: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read admin notifications: %w", err)
	}

	unread, err := CountUnreadAdminNotifications(ctx, database, siteID)
	if err != nil {
		return nil, 0, err
	}
	return items, unread, nil
}

func CountUnreadAdminNotifications(ctx context.Context, database *sql.DB, siteID int64) (int64, error) {
	var unread int64
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM "`+AdminNotificationTable+`"
		WHERE "site_id" = ? AND "read_at" = ''`, siteID).Scan(&unread); err != nil {
		return 0, fmt.Errorf("count unread admin notifications: %w", err)
	}
	return unread, nil
}

// MarkAdminNotificationsRead marks the given entries (or every unread entry of
// the site when ids is empty) as read.
func MarkAdminNotificationsRead(ctx context.Context, database *sql.DB, siteID int64, ids []int64) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if len(ids) == 0 {
		if _, err := database.ExecContext(ctx, `
			UPDATE "`+AdminNotificationTable+`" SET "read_at" = ?
			WHERE "site_id" = ? AND "read_at" = ''`, now, siteID); err != nil {
			return fmt.Errorf("mark admin notifications read: %w", err)
		}
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, siteID)
	for index, id := range ids {
		placeholders[index] = "?"
		args = append(args, id)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "`+AdminNotificationTable+`" SET "read_at" = ?
		WHERE "site_id" = ? AND "read_at" = '' AND "id" IN (`+strings.Join(placeholders, ",")+`)`,
		args...); err != nil {
		return fmt.Errorf("mark admin notifications read: %w", err)
	}
	return nil
}
