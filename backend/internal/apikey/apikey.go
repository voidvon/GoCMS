package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gocms/internal/db"
)

const (
	KeyPrefix                 = "cms_live_"
	SecretBytes               = 32
	UsageTouchIntervalSeconds = 5 * 60
)

var (
	ErrNotFound       = errors.New("API Key 不存在")
	ErrRevoked        = errors.New("已撤销的 API Key 不能轮换")
	ErrExpired        = errors.New("已过期的 API Key 不能轮换")
	ErrNameRequired   = errors.New("请输入 API Key 名称")
	ErrNameTooLong    = errors.New("API Key 名称不能超过 120 个字符")
	ErrExpiresInvalid = errors.New("API Key 过期时间无效")
	ErrExpiresPast    = errors.New("API Key 过期时间必须晚于当前时间")
	ErrAdminNotFound  = errors.New("管理员不存在")
)

type ApiKey struct {
	ID                int64   `json:"id"`
	AdminID           int64   `json:"admin_id"`
	AdminUsername     string  `json:"admin_username"`
	Name              string  `json:"name"`
	KeyPrefix         string  `json:"key_prefix"`
	ExpiresAt         *string `json:"expires_at"`
	RevokedAt         *string `json:"revoked_at"`
	RevokedByAdminID  *int64  `json:"revoked_by_admin_id"`
	CreatedByAdminID  *int64  `json:"created_by_admin_id"`
	CreatedByUsername string  `json:"created_by_username"`
	LastUsedAt        *string `json:"last_used_at"`
	LastUsedIP        string  `json:"last_used_ip"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	Status            string  `json:"status"`
}

type ApiKeyWithSecret struct {
	ApiKey
	Key string `json:"key"`
}

type ApiKeyEvent struct {
	ID            int64          `json:"id"`
	ApiKeyID      *int64         `json:"api_key_id"`
	ActorAdminID  *int64         `json:"actor_admin_id"`
	ActorUsername string         `json:"actor_username"`
	EventType     string         `json:"event_type"`
	ClientIP      string         `json:"client_ip"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     string         `json:"created_at"`
}

type AuthIdentity struct {
	ApiKeyID int64
	AdminID  int64
	Username string
	Flags    string
	Name     string
}

type CreateInput struct {
	Name      string  `json:"name"`
	ExpiresAt *string `json:"expires_at"`
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateToken() (token string, keyPrefix string, keyHash string, err error) {
	bytes := make([]byte, SecretBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	token = KeyPrefix + base64.RawURLEncoding.EncodeToString(bytes)
	prefixLen := len(KeyPrefix) + 12
	if prefixLen > len(token) {
		prefixLen = len(token)
	}
	keyPrefix = token[:prefixLen]
	keyHash = HashToken(token)
	return token, keyPrefix, keyHash, nil
}

func normalizeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrNameRequired
	}
	if len([]rune(trimmed)) > 120 {
		return "", ErrNameTooLong
	}
	return trimmed, nil
}

func normalizeExpiresAt(expiresAt *string) (*string, error) {
	if expiresAt == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*expiresAt)
	if trimmed == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		if t, err = time.Parse("2006-01-02 15:04:05", trimmed); err != nil {
			if t, err = time.Parse("2006-01-02", trimmed); err != nil {
				return nil, ErrExpiresInvalid
			}
		}
	}
	if !t.After(time.Now()) {
		return nil, ErrExpiresPast
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted, nil
}

func resolveStatus(revokedAt *string, expiresAt *string) string {
	if revokedAt != nil && strings.TrimSpace(*revokedAt) != "" {
		return "revoked"
	}
	if expiresAt != nil && strings.TrimSpace(*expiresAt) != "" {
		t, err := time.Parse(time.RFC3339, *expiresAt)
		if err == nil && !t.After(time.Now()) {
			return "expired"
		}
	}
	return "active"
}

func assertAdminExists(ctx context.Context, database *sql.DB, adminID int64) error {
	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_admin_user" WHERE "id" = ?`, adminID).Scan(&count)
	if err != nil {
		return fmt.Errorf("check admin exists: %w", err)
	}
	if count == 0 {
		return ErrAdminNotFound
	}
	return nil
}

func recordEvent(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, apiKeyID int64, actorAdminID int64, eventType string, clientIP string, metadata map[string]any) error {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = execer.ExecContext(ctx, `
		INSERT INTO "`+db.ApiKeyEventTable+`" (
			"api_key_id",
			"actor_admin_id",
			"event_type",
			"client_ip",
			"metadata_json",
			"created_at"
		) VALUES (?, ?, ?, ?, ?, ?)`,
		apiKeyID, actorAdminID, eventType, strings.TrimSpace(clientIP), string(metaJSON), now)
	return err
}

func Create(ctx context.Context, database *sql.DB, adminID int64, creatorAdminID int64, input CreateInput, clientIP string) (*ApiKeyWithSecret, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}
	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, err
	}
	expiresAt, err := normalizeExpiresAt(input.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if err := assertAdminExists(ctx, database, adminID); err != nil {
		return nil, err
	}
	if err := assertAdminExists(ctx, database, creatorAdminID); err != nil {
		return nil, err
	}

	token, keyPrefix, keyHash, err := generateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO "`+db.ApiKeyTable+`" (
			"admin_id",
			"name",
			"key_prefix",
			"key_hash",
			"expires_at",
			"created_by_admin_id",
			"created_at",
			"updated_at"
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		adminID, name, keyPrefix, keyHash, expiresAt, creatorAdminID, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get api key last insert id: %w", err)
	}

	if err := recordEvent(ctx, tx, id, creatorAdminID, "created", clientIP, map[string]any{}); err != nil {
		return nil, fmt.Errorf("record api key created event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	apiKey, err := GetByID(ctx, database, id)
	if err != nil {
		return nil, err
	}

	return &ApiKeyWithSecret{
		ApiKey: *apiKey,
		Key:    token,
	}, nil
}

func Authenticate(ctx context.Context, database *sql.DB, token string) (*AuthIdentity, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(token)
	if trimmed == "" || len(trimmed) > 256 {
		return nil, nil
	}

	keyHash := HashToken(trimmed)
	now := time.Now().UTC().Format(time.RFC3339)

	var (
		ident     AuthIdentity
		keyPrefix string
		expiresAt sql.NullString
		lastUsed  sql.NullString
		lastIP    sql.NullString
	)

	err := database.QueryRowContext(ctx, `
		SELECT
			k."id",
			k."admin_id",
			u."username",
			COALESCE(u."flags", ''),
			k."name",
			k."key_prefix",
			k."expires_at",
			k."last_used_at",
			k."last_used_ip"
		FROM "`+db.ApiKeyTable+`" k
		JOIN "gocms_admin_user" u ON u."id" = k."admin_id"
		WHERE k."key_hash" = ?
		  AND k."revoked_at" IS NULL
		  AND (k."expires_at" IS NULL OR k."expires_at" > ?)`,
		keyHash, now).Scan(
		&ident.ApiKeyID,
		&ident.AdminID,
		&ident.Username,
		&ident.Flags,
		&ident.Name,
		&keyPrefix,
		&expiresAt,
		&lastUsed,
		&lastIP,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate api key: %w", err)
	}

	return &ident, nil
}

func TouchUsage(ctx context.Context, database *sql.DB, apiKeyID int64, clientIP string) {
	if database == nil || apiKeyID <= 0 {
		return
	}
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(UsageTouchIntervalSeconds) * time.Second).Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	_, _ = database.ExecContext(ctx, `
		UPDATE "`+db.ApiKeyTable+`"
		SET
			"last_used_at" = ?,
			"last_used_ip" = ?,
			"updated_at" = ?
		WHERE "id" = ?
		  AND (
			"last_used_at" IS NULL
			OR "last_used_at" <= ?
		  )`,
		nowStr, strings.TrimSpace(clientIP), nowStr, apiKeyID, cutoff)
}

func List(ctx context.Context, database *sql.DB, adminID *int64) ([]ApiKey, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}

	query := `
		SELECT
			k."id",
			k."admin_id",
			k."name",
			k."key_prefix",
			k."expires_at",
			k."revoked_at",
			k."revoked_by_admin_id",
			k."created_by_admin_id",
			k."last_used_at",
			k."last_used_ip",
			k."created_at",
			k."updated_at",
			COALESCE(owner."username", '') AS admin_username,
			COALESCE(creator."username", '') AS created_by_username
		FROM "` + db.ApiKeyTable + `" k
		JOIN "gocms_admin_user" owner ON owner."id" = k."admin_id"
		LEFT JOIN "gocms_admin_user" creator ON creator."id" = k."created_by_admin_id" `

	var (
		rows *sql.Rows
		err  error
	)
	if adminID != nil {
		query += `WHERE k."admin_id" = ? ORDER BY k."created_at" DESC, k."id" DESC`
		rows, err = database.QueryContext(ctx, query, *adminID)
	} else {
		query += `ORDER BY k."created_at" DESC, k."id" DESC`
		rows, err = database.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query api keys: %w", err)
	}
	defer rows.Close()

	items := make([]ApiKey, 0)
	for rows.Next() {
		var item ApiKey
		var (
			expiresAt   sql.NullString
			revokedAt   sql.NullString
			revokedBy   sql.NullInt64
			createdBy   sql.NullInt64
			lastUsedAt  sql.NullString
			lastUsedIP  sql.NullString
		)
		if err := rows.Scan(
			&item.ID,
			&item.AdminID,
			&item.Name,
			&item.KeyPrefix,
			&expiresAt,
			&revokedAt,
			&revokedBy,
			&createdBy,
			&lastUsedAt,
			&lastUsedIP,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.AdminUsername,
			&item.CreatedByUsername,
		); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}

		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.String
		}
		if revokedAt.Valid {
			item.RevokedAt = &revokedAt.String
		}
		if revokedBy.Valid {
			item.RevokedByAdminID = &revokedBy.Int64
		}
		if createdBy.Valid {
			item.CreatedByAdminID = &createdBy.Int64
		}
		if lastUsedAt.Valid {
			item.LastUsedAt = &lastUsedAt.String
		}
		item.LastUsedIP = lastUsedIP.String
		item.Status = resolveStatus(item.RevokedAt, item.ExpiresAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}

	return items, nil
}

func GetByID(ctx context.Context, database *sql.DB, id int64) (*ApiKey, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}

	var item ApiKey
	var (
		expiresAt  sql.NullString
		revokedAt  sql.NullString
		revokedBy  sql.NullInt64
		createdBy  sql.NullInt64
		lastUsedAt sql.NullString
		lastUsedIP sql.NullString
	)

	err := database.QueryRowContext(ctx, `
		SELECT
			k."id",
			k."admin_id",
			k."name",
			k."key_prefix",
			k."expires_at",
			k."revoked_at",
			k."revoked_by_admin_id",
			k."created_by_admin_id",
			k."last_used_at",
			k."last_used_ip",
			k."created_at",
			k."updated_at",
			COALESCE(owner."username", '') AS admin_username,
			COALESCE(creator."username", '') AS created_by_username
		FROM "`+db.ApiKeyTable+`" k
		JOIN "gocms_admin_user" owner ON owner."id" = k."admin_id"
		LEFT JOIN "gocms_admin_user" creator ON creator."id" = k."created_by_admin_id"
		WHERE k."id" = ?`, id).Scan(
		&item.ID,
		&item.AdminID,
		&item.Name,
		&item.KeyPrefix,
		&expiresAt,
		&revokedAt,
		&revokedBy,
		&createdBy,
		&lastUsedAt,
		&lastUsedIP,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.AdminUsername,
		&item.CreatedByUsername,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}

	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.String
	}
	if revokedAt.Valid {
		item.RevokedAt = &revokedAt.String
	}
	if revokedBy.Valid {
		item.RevokedByAdminID = &revokedBy.Int64
	}
	if createdBy.Valid {
		item.CreatedByAdminID = &createdBy.Int64
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.String
	}
	item.LastUsedIP = lastUsedIP.String
	item.Status = resolveStatus(item.RevokedAt, item.ExpiresAt)

	return &item, nil
}

func Rotate(ctx context.Context, database *sql.DB, id int64, actorAdminID int64, clientIP string) (*ApiKeyWithSecret, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}
	existing, err := GetByID(ctx, database, id)
	if err != nil {
		return nil, err
	}
	if existing.RevokedAt != nil && *existing.RevokedAt != "" {
		return nil, ErrRevoked
	}
	if existing.Status == "expired" {
		return nil, ErrExpired
	}

	if err := assertAdminExists(ctx, database, existing.AdminID); err != nil {
		return nil, err
	}
	if err := assertAdminExists(ctx, database, actorAdminID); err != nil {
		return nil, err
	}

	token, keyPrefix, keyHash, err := generateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO "`+db.ApiKeyTable+`" (
			"admin_id",
			"name",
			"key_prefix",
			"key_hash",
			"expires_at",
			"created_by_admin_id",
			"created_at",
			"updated_at"
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		existing.AdminID, existing.Name, keyPrefix, keyHash, existing.ExpiresAt, actorAdminID, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert rotated api key: %w", err)
	}

	newKeyID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get rotated key id: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE "`+db.ApiKeyTable+`"
		SET
			"revoked_at" = ?,
			"revoked_by_admin_id" = ?,
			"updated_at" = ?
		WHERE "id" = ?
		  AND "revoked_at" IS NULL`,
		now, actorAdminID, now, id)
	if err != nil {
		return nil, fmt.Errorf("revoke old key during rotation: %w", err)
	}

	if err := recordEvent(ctx, tx, id, actorAdminID, "rotated", clientIP, map[string]any{
		"replacement_id": newKeyID,
	}); err != nil {
		return nil, fmt.Errorf("record rotated event: %w", err)
	}

	if err := recordEvent(ctx, tx, newKeyID, actorAdminID, "created", clientIP, map[string]any{
		"replacement_for_id": id,
	}); err != nil {
		return nil, fmt.Errorf("record replacement created event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rotation: %w", err)
	}

	newKey, err := GetByID(ctx, database, newKeyID)
	if err != nil {
		return nil, err
	}

	return &ApiKeyWithSecret{
		ApiKey: *newKey,
		Key:    token,
	}, nil
}

func Revoke(ctx context.Context, database *sql.DB, id int64, actorAdminID int64, clientIP string, reason string) (*ApiKey, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}
	existing, err := GetByID(ctx, database, id)
	if err != nil {
		return nil, err
	}
	if existing.RevokedAt != nil && *existing.RevokedAt != "" {
		return existing, nil
	}
	if err := assertAdminExists(ctx, database, actorAdminID); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE "`+db.ApiKeyTable+`"
		SET
			"revoked_at" = ?,
			"revoked_by_admin_id" = ?,
			"updated_at" = ?
		WHERE "id" = ?
		  AND "revoked_at" IS NULL`,
		now, actorAdminID, now, id)
	if err != nil {
		return nil, fmt.Errorf("revoke api key: %w", err)
	}

	meta := map[string]any{}
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		meta["reason"] = trimmedReason
	}
	if err := recordEvent(ctx, tx, id, actorAdminID, "revoked", clientIP, meta); err != nil {
		return nil, fmt.Errorf("record revoked event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit revoke: %w", err)
	}

	return GetByID(ctx, database, id)
}

func ListEvents(ctx context.Context, database *sql.DB, apiKeyID int64) ([]ApiKeyEvent, error) {
	if err := db.EnsureApiKeys(ctx, database); err != nil {
		return nil, err
	}

	if _, err := GetByID(ctx, database, apiKeyID); err != nil {
		return nil, err
	}

	rows, err := database.QueryContext(ctx, `
		SELECT
			e."id",
			e."api_key_id",
			e."actor_admin_id",
			COALESCE(a."username", '') AS actor_username,
			e."event_type",
			e."client_ip",
			e."metadata_json",
			e."created_at"
		FROM "`+db.ApiKeyEventTable+`" e
		LEFT JOIN "gocms_admin_user" a ON a."id" = e."actor_admin_id"
		WHERE e."api_key_id" = ?
		ORDER BY e."created_at" DESC, e."id" DESC`, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("query api key events: %w", err)
	}
	defer rows.Close()

	events := make([]ApiKeyEvent, 0)
	for rows.Next() {
		var (
			event    ApiKeyEvent
			keyID    sql.NullInt64
			actorID  sql.NullInt64
			metaJSON string
		)
		if err := rows.Scan(
			&event.ID,
			&keyID,
			&actorID,
			&event.ActorUsername,
			&event.EventType,
			&event.ClientIP,
			&metaJSON,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api key event: %w", err)
		}
		if keyID.Valid {
			event.ApiKeyID = &keyID.Int64
		}
		if actorID.Valid {
			event.ActorAdminID = &actorID.Int64
		}
		meta := make(map[string]any)
		if err := json.Unmarshal([]byte(metaJSON), &meta); err == nil {
			event.Metadata = meta
		} else {
			event.Metadata = map[string]any{}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api key events: %w", err)
	}

	return events, nil
}
