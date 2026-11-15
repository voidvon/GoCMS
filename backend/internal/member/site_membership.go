package member

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// joinMemberSite runs in the login transaction. Site association and its default
// group cannot be committed for a login rejected by limits or response failures.
func joinMemberSite(ctx context.Context, tx *sql.Tx, id, site int64, registered bool) error {
	var origin int64
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT site_id,site_ids FROM gocms_user WHERE id=? AND status='active'`, id).Scan(&origin, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnauthorized
		}
		return err
	}
	var sites []int64
	if err := json.Unmarshal([]byte(raw), &sites); err != nil {
		return ErrInvalid
	}
	hasSite, hasOrigin := origin == site, false
	for _, sid := range sites {
		hasSite = hasSite || sid == site
		hasOrigin = hasOrigin || sid == origin
	}
	if !hasOrigin {
		sites = append(sites, origin)
	}
	if !hasSite {
		sites = append(sites, site)
	}
	if !hasSite || !hasOrigin {
		encoded, err := json.Marshal(sites)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE gocms_user SET site_ids=? WHERE id=?`, string(encoded), id); err != nil {
			return err
		}
	}
	if !hasSite || registered {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gocms_user_group_member(user_id,group_id,status) SELECT ?,id,'active' FROM gocms_user_group WHERE site_id=? AND is_default=1 AND status='active'`, id, site)
		return err
	}
	return nil
}
