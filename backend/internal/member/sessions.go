package member

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// SessionLifetime is the policy for every member session, regardless of login
// provider or HTTP transport. Only digests of session credentials are persisted.
const SessionLifetime = 30 * 24 * time.Hour

var ErrSessionLimit = errors.New("session limit reached")

// SessionGrant is returned only when a session is issued. JSON serialization
// deliberately excludes it; HTTP adapters control delivery of the secret.
type SessionGrant struct {
	Token     string    `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

// Identity carries the authenticated profile and a public session reference.
// It contains neither a raw credential nor its digest.
type Identity struct {
	Profile
	SessionID string    `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

func TokenHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(v[:])
}

// createSession is shared by password and verified external identity logins.
// The caller holds a transaction; status, credential snapshot and limits are rechecked.
func createSession(ctx context.Context, tx *sql.Tx, id int64, encoded, identifier, ip, agent, currentToken string) (SessionGrant, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return SessionGrant{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	expiresAt := time.Now().Add(SessionLifetime).Truncate(time.Second)
	var err error
	// Acquire the SQLite write lock before counting to serialize concurrent logins.
	if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return SessionGrant{}, err
	}
	var limit int
	if err = tx.QueryRowContext(ctx, "SELECT max_sessions FROM gocms_user WHERE id=? AND status='active' AND password_hash=?", id, encoded).Scan(&limit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionGrant{}, ErrUnauthorized
		}
		return SessionGrant{}, err
	}
	// Only the supplied live session belonging to this account can be replaced.
	if currentToken != "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE user_id=? AND token_hash=?", id, TokenHash(currentToken)); err != nil {
			return SessionGrant{}, err
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM gocms_user_session WHERE user_id=?", id).Scan(&count); err != nil {
		return SessionGrant{}, err
	}
	if count >= limit {
		return SessionGrant{}, ErrSessionLimit
	}
	// Recheck the hash/status so concurrent disable or password change cannot create
	// a session after revocation.
	result, err := tx.ExecContext(ctx, "INSERT INTO gocms_user_session(token_hash,user_id,expires_at,ip,user_agent) SELECT ?,id,?,?,? FROM gocms_user WHERE id=? AND status='active' AND password_hash=?", TokenHash(token), expiresAt.Unix(), ip, agent, id, encoded)
	if err != nil {
		return SessionGrant{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return SessionGrant{}, err
	}
	if n != 1 {
		return SessionGrant{}, ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return SessionGrant{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE gocms_user SET last_login_at=CURRENT_TIMESTAMP,last_login_ip=? WHERE id=?", ip, id); err != nil {
		return SessionGrant{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gocms_user_login(user_id,identifier,success,ip) VALUES(?,?,1,?)", id, identifier, ip); err != nil {
		return SessionGrant{}, err
	}
	return SessionGrant{Token: token, ExpiresAt: expiresAt}, nil
}

func (s Service) Authenticate(ctx context.Context, token string, siteIDOpt ...int64) (Identity, error) {
	var identity Identity
	p := &identity.Profile
	var expires int64
	var originSiteID int64
	var status, rawSiteIDs string
	query := `SELECT u.id, u.site_id, u.username, u.email, u.display_name, u.avatar_url, u.status, COALESCE(u.site_ids, '[]'), u.password_hash<>'', s.expires_at
	          FROM gocms_user u
	          JOIN gocms_user_session s ON s.user_id=u.id
	          WHERE s.token_hash=? AND s.expires_at>?`
	tokenHash := TokenHash(token)
	args := []any{tokenHash, time.Now().Unix()}

	err := s.DB.QueryRowContext(ctx, query, args...).Scan(&p.ID, &originSiteID, &p.Username, &p.Email, &p.DisplayName, &p.AvatarURL, &status, &rawSiteIDs, &p.HasPassword, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrUnauthorized
	}
	if err != nil {
		return Identity{}, err
	}
	if status != "active" {
		return Identity{}, ErrUnauthorized
	}

	// Auto-join site on authenticate
	if len(siteIDOpt) > 0 && siteIDOpt[0] > 0 {
		targetSiteID := siteIDOpt[0]
		var sids []int64
		if err := json.Unmarshal([]byte(rawSiteIDs), &sids); err != nil {
			return Identity{}, errors.New("invalid stored account site memberships")
		}
		hasOrigin := false
		hasTarget := false
		for _, sid := range sids {
			if sid == originSiteID {
				hasOrigin = true
			}
			if sid == targetSiteID {
				hasTarget = true
			}
		}
		needsUpdate := false
		if !hasOrigin && originSiteID > 0 {
			sids = append(sids, originSiteID)
			needsUpdate = true
		}
		if !hasTarget {
			sids = append(sids, targetSiteID)
			needsUpdate = true
		}
		if needsUpdate {
			newJSON, _ := json.Marshal(sids)
			if _, err := s.DB.ExecContext(ctx, "UPDATE gocms_user SET site_ids=? WHERE id=?", string(newJSON), p.ID); err != nil {
				return Identity{}, err
			}
		}
	}

	identity.SessionID = SessionID(tokenHash)
	identity.ExpiresAt = time.Unix(expires, 0).UTC()
	return identity, nil
}
func (s Service) Logout(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM gocms_user_session WHERE token_hash=?", TokenHash(token))
	return err
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
		v.Current = v.ID == current
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
