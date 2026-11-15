package member

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"gocms/internal/auth"
	"gocms/internal/memberoauth"
)

type ExternalLoginInput struct {
	SiteID             int64
	Provider           string
	Revision           string
	Subject            string
	Email              string
	DisplayName        string
	EmailVerified      bool
	BindUserID         int64
	BindingSessionHash string
	IP                 string
	UserAgent          string
	CurrentToken       string
}

// ExternalProviderLogin accepts only identity claims verified by the provider
// adapter. Configuration, binding and session checks share one transaction.
func (s Service) ExternalProviderLogin(ctx context.Context, in ExternalLoginInput) (LoginResult, error) {
	site, provider, revision := in.SiteID, in.Provider, in.Revision
	subject, email, name, verified := in.Subject, in.Email, in.DisplayName, in.EmailVerified
	bindUser, bindingSession := in.BindUserID, in.BindingSessionHash
	ip, agent, current := in.IP, in.UserAgent, in.CurrentToken
	if !memberoauth.Supported(provider) {
		return LoginResult{}, ErrInvalid
	}
	issuer := memberoauth.Config{Provider: provider}.Issuer()
	if subject == "" || len(subject) > 255 {
		return LoginResult{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()
	// Take the write lock before loading configuration so disable/update is serialized.
	if _, err = tx.ExecContext(ctx, `DELETE FROM gocms_member_oauth_state WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		return LoginResult{}, err
	}
	var auto bool
	var clientID string
	if err = tx.QueryRowContext(ctx, `SELECT auto_register,client_id FROM gocms_platform_oauth_config c JOIN gocms_site s ON s.id=? WHERE c.provider=? AND c.enabled=1 AND c.revision=? AND s.status='active'`, site, provider, revision).Scan(&auto, &clientID); err != nil {
		return LoginResult{}, ErrUnauthorized
	}
	if provider == "facebook" || provider == "microsoft" {
		issuer += "/" + clientID
	}
	id, err := resolveProviderIdentity(ctx, tx, provider, issuer, subject)
	if err != nil {
		return LoginResult{}, err
	}
	if bindUser > 0 {
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_user_session s JOIN gocms_user u ON u.id=s.user_id WHERE s.user_id=? AND s.token_hash=? AND s.expires_at>? AND u.status='active' AND (u.site_id=? OR EXISTS(SELECT 1 FROM json_each(u.site_ids) WHERE value=?))`, bindUser, bindingSession, time.Now().Unix(), site, site).Scan(&active); err != nil || active != 1 {
			return LoginResult{}, ErrUnauthorized
		}
		if id != 0 && id != bindUser {
			return LoginResult{}, ErrConflict
		}
		id = bindUser
		if _, err = tx.ExecContext(ctx, `INSERT INTO gocms_platform_identity(user_id,provider,issuer,subject) VALUES(?,?,?,?) ON CONFLICT(provider,issuer,subject) DO NOTHING`, id, provider, issuer, subject); err != nil {
			return LoginResult{}, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO gocms_user_event(user_id,action) VALUES(?,?)`, id, provider+"_bound"); err != nil {
			return LoginResult{}, err
		}
		result, err := prepareLoginResult(ctx, tx, id, site, SessionGrant{})
		if err != nil {
			return LoginResult{}, err
		}
		if err = tx.Commit(); err != nil {
			return LoginResult{}, err
		}
		return result, nil
	}
	registered := false
	if id == 0 {
		if !auto {
			return LoginResult{}, ErrUnauthorized
		}
		if provider == "facebook" || provider == "microsoft" {
			// Use the verified app-scoped subject for identity. These providers
			// do not supply an email ownership guarantee suitable for account linking.
			email = ""
		} else {
			email = strings.ToLower(strings.TrimSpace(email))
			a, e := mail.ParseAddress(email)
			if !verified || e != nil || a.Address != email || len(email) > 254 {
				return LoginResult{}, ErrInvalid
			}
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_user WHERE lower(email)=?`, email).Scan(&count); err != nil {
				return LoginResult{}, err
			}
			if count > 0 {
				return LoginResult{}, ErrConflict
			}
		}
		random, e := memberoauth.Random()
		if e != nil {
			return LoginResult{}, e
		}
		username := provider + "_" + random[:24]
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 200 {
			name = username
		}
		result, e := tx.ExecContext(ctx, `INSERT INTO gocms_user(site_id,site_ids,username,email,password_hash,display_name) VALUES(?,?,?,?, '',?)`, site, fmt.Sprintf("[%d]", site), username, email, name)
		if e != nil {
			return LoginResult{}, ErrConflict
		}
		id, e = result.LastInsertId()
		registered = true
		if e != nil {
			return LoginResult{}, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO gocms_platform_identity(user_id,provider,issuer,subject) VALUES(?,?,?,?)`, id, provider, issuer, subject); err != nil {
			return LoginResult{}, err
		}
	}
	// A verified shared identity joins only this site's default group. Memberships
	// and entitlements in other sites are never copied.
	if err = joinMemberSite(ctx, tx, id, site, registered); err != nil {
		return LoginResult{}, err
	}

	var hash string
	if err = tx.QueryRowContext(ctx, `SELECT password_hash FROM gocms_user WHERE id=? AND status='active' AND (site_id=? OR EXISTS(SELECT 1 FROM json_each(site_ids) WHERE value=?))`, id, site, site).Scan(&hash); err != nil {
		return LoginResult{}, ErrUnauthorized
	}
	session, err := createSession(ctx, tx, id, hash, provider, ip, agent, current)
	if err != nil {
		return LoginResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE gocms_platform_identity SET last_login_at=CURRENT_TIMESTAMP WHERE user_id=? AND provider=?`, id, provider); err != nil {
		return LoginResult{}, err
	}
	result, err := prepareLoginResult(ctx, tx, id, site, session)
	if err != nil {
		return LoginResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

// ConfirmExternalChange requires the local password or a recently issued session
// for passwordless accounts. The Google authorization itself supplies the other proof.
func (s Service) ConfirmExternalChange(ctx context.Context, id int64, token, password string) error {
	if len(password) > 1024 {
		return ErrInvalid
	}
	var hash string
	var created string
	err := s.DB.QueryRowContext(ctx, `SELECT u.password_hash,s.created_at FROM gocms_user u JOIN gocms_user_session s ON s.user_id=u.id WHERE u.id=? AND u.status='active' AND s.token_hash=? AND s.expires_at>?`, id, TokenHash(token), time.Now().Unix()).Scan(&hash, &created)
	if err != nil {
		return ErrUnauthorized
	}
	if hash != "" {
		if !auth.ComparePassword(password, hash) {
			return ErrUnauthorized
		}
		return nil
	}
	t, e := ParseTime(created)
	if e != nil || time.Since(t) > 10*time.Minute {
		return ErrUnauthorized
	}
	return nil
}
func (s Service) ExternalStatus(ctx context.Context, id int64) (bool, bool, error) {
	return s.ExternalProviderStatus(ctx, id, "google")
}
func (s Service) ExternalProviderStatus(ctx context.Context, id int64, provider string) (bool, bool, error) {
	var bound, hasPassword bool
	err := s.DB.QueryRowContext(ctx, `SELECT (EXISTS(SELECT 1 FROM gocms_platform_identity WHERE user_id=? AND provider=?) OR EXISTS(SELECT 1 FROM gocms_member_identity WHERE user_id=? AND provider=?)),password_hash<>'' FROM gocms_user WHERE id=? AND status='active'`, id, provider, id, provider, id).Scan(&bound, &hasPassword)
	return bound, hasPassword, err
}
func (s Service) UnbindGoogle(ctx context.Context, id int64, token, password string) error {
	return s.UnbindProvider(ctx, id, "google", token, password)
}
func (s Service) UnbindProvider(ctx context.Context, id int64, provider, token, password string) error {
	if len(password) > 1024 {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize against revocation/password changes, then confirm inside the transaction.
	if _, err = tx.ExecContext(ctx, `DELETE FROM gocms_user_session WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		return err
	}
	var hash string
	if err = tx.QueryRowContext(ctx, `SELECT u.password_hash FROM gocms_user u JOIN gocms_user_session s ON s.user_id=u.id WHERE u.id=? AND s.token_hash=? AND s.expires_at>? AND u.status='active'`, id, TokenHash(token), time.Now().Unix()).Scan(&hash); err != nil {
		return ErrUnauthorized
	}
	if hash == "" {
		return ErrInvalid
	}
	if !auth.ComparePassword(password, hash) {
		return ErrUnauthorized
	}
	// Unlink the platform identity and this account's legacy bindings together.
	result, err := tx.ExecContext(ctx, `DELETE FROM gocms_platform_identity WHERE user_id=? AND provider=?`, id, provider)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM gocms_member_identity WHERE user_id=? AND provider=?`, id, provider)
	if err != nil {
		return err
	}
	legacy, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n+legacy == 0 {
		return ErrInvalid
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO gocms_user_event(user_id,action) VALUES(?,?)`, id, provider+"_unbound"); err != nil {
		return err
	}
	return tx.Commit()
}

// Promote only an unambiguous legacy binding. Conflicting accounts must never be
// merged or selected by site order, email, or a fixed identifier.
func resolveExternalIdentity(ctx context.Context, tx *sql.Tx, subject string) (int64, error) {
	return resolveProviderIdentity(ctx, tx, "google", memberoauth.Issuer, subject)
}
func resolveProviderIdentity(ctx context.Context, tx *sql.Tx, provider, issuer, subject string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM gocms_platform_identity WHERE provider=? AND issuer=? AND subject=?`, provider, issuer, subject).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(user_id),0),COUNT(DISTINCT user_id) FROM gocms_member_identity WHERE provider=? AND issuer=? AND subject=?`, provider, issuer, subject).Scan(&id, &count); err != nil {
		return 0, err
	}
	if count > 1 {
		return 0, ErrConflict
	}
	if count == 0 {
		return 0, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO gocms_platform_identity(user_id,provider,issuer,subject) VALUES(?,?,?,?)`, id, provider, issuer, subject); err != nil {
		return 0, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM gocms_member_identity WHERE provider=? AND issuer=? AND subject=?`, provider, issuer, subject); err != nil {
		return 0, err
	}
	return id, nil
}
