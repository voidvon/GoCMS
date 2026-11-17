package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/netip"
	"time"

	"gocms/internal/nginxguard"
)

type SyncStatus struct {
	State     string `json:"state"`
	Digest    string `json:"digest"`
	AppliedAt string `json:"applied_at"`
	Error     string `json:"error"`
}

// SyncError describes an operational synchronization failure. The admin API
// returns it as a conflict so gateways preserve the application's explanation.
type SyncError struct{ Err error }

func (e *SyncError) Error() string { return e.Err.Error() }
func (e *SyncError) Unwrap() error { return e.Err }

func (s *Service) desired(ctx context.Context) (*state, nginxguard.Snapshot, error) {
	v, err := s.current(ctx)
	if err != nil {
		return nil, nginxguard.Snapshot{}, err
	}
	snapshot := v.snapshot
	if !v.platform.NginxEnabled {
		snapshot = nginxguard.Snapshot{Tenants: []nginxguard.Tenant{}, Rules: []nginxguard.Rule{}}
	}
	return v, snapshot, nil
}

func (s *Service) Status(ctx context.Context) SyncStatus {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	v, want, err := s.desired(ctx)
	if err != nil {
		return SyncStatus{State: "failed", Error: err.Error()}
	}
	status := s.status
	if !v.platform.NginxEnabled && status.Digest == "" && status.State != "failed" {
		status.State = "disabled"
		return status
	}
	if status.State == "failed" {
		return status
	}
	if status.Digest != nginxguard.Digest(want) {
		status.State = "pending"
	} else if v.platform.NginxEnabled {
		status.State = "applied"
	} else {
		status.State = "disabled"
	}
	return status
}

func (s *Service) Sync(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	v, want, err := s.desired(ctx)
	if err != nil {
		return err
	}
	if !v.platform.NginxEnabled && s.status.Digest == "" {
		return &SyncError{Err: errors.New("请先保存并启用 Nginx 同步")}
	}
	result, err := s.controller.Apply(ctx, v.platform.ControlConfig(), want)
	if err != nil {
		s.status.State = "failed"
		s.status.Error = err.Error()
		return &SyncError{Err: err}
	}
	if result.Digest != nginxguard.Digest(want) {
		s.status.State = "failed"
		s.status.Error = "Nginx 管理器未确认当前名单"
		return &SyncError{Err: errors.New(s.status.Error)}
	}
	s.status = SyncStatus{State: "applied", Digest: result.Digest, AppliedAt: result.AppliedAt}
	raw, err := json.Marshal(s.status)
	if err != nil {
		return err
	}
	if _, err := s.database.ExecContext(ctx, `INSERT INTO gocms_security_state(key,value) VALUES('nginx_status',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); err != nil {
		return err
	}
	return nil
}

func (s *Service) Preview(ctx context.Context) (string, string, error) {
	_, want, err := s.desired(ctx)
	if err != nil {
		return "", "", err
	}
	return nginxguard.SingleFileFragments(want, time.Now(), "/var/log/gocms-security/probes.log")
}

// Pull Nginx-only probes as well: Nginx may reject .env/.git before they reach
// Go. The server logs only requests that never selected an upstream.
func (s *Service) pull(ctx context.Context, p Platform) error {
	var cursor string
	err := s.database.QueryRowContext(ctx, `SELECT value FROM gocms_security_state WHERE key='cursor'`).Scan(&cursor)
	if err != nil {
		cursor = ""
	}
	batch, err := s.controller.Events(ctx, p.ControlConfig(), cursor)
	if err != nil {
		return err
	}
	for _, e := range batch.Items {
		when, err := time.Parse(time.RFC3339, e.Time)
		if err != nil || time.Since(when) > 5*time.Minute || when.After(time.Now().Add(time.Minute)) {
			continue
		}
		ip, err := netip.ParseAddr(e.IP)
		if err != nil {
			continue
		}
		if _, err := s.Observe(ctx, e.Host, e.Path, ip.Unmap()); err != nil {
			return err
		}
	}
	_, err = s.database.ExecContext(ctx, `INSERT INTO gocms_security_state(key,value) VALUES('cursor',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, batch.Cursor)
	return err
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			v, err := s.current(ctx)
			if err != nil {
				log.Printf("security configuration: %v", err)
				continue
			}
			if !v.platform.NginxEnabled {
				if status := s.Status(ctx); status.Digest == "" || status.State == "disabled" {
					continue
				}
			}
			if err := s.Sync(ctx); err != nil {
				continue
			}
			if v.platform.NginxEnabled {
				if err := s.pull(ctx, v.platform); err != nil {
					log.Printf("security probes: %v", err)
				}
			}
		case <-cleanup.C:
			cutoff := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
			_, err := s.database.ExecContext(ctx, `DELETE FROM gocms_security_event WHERE created_at<?`, cutoff)
			if err != nil {
				log.Printf("security cleanup: %v", err)
			}
			_, err = s.database.ExecContext(ctx, `DELETE FROM gocms_security_entry WHERE (revoked_at<>'' AND revoked_at<?) OR (expires_at<>'' AND expires_at<?)`, cutoff, cutoff)
			if err != nil {
				log.Printf("security cleanup: %v", err)
			}
		}
	}
}

func (s *Service) NginxSetup(ctx context.Context) nginxguard.SetupStatus {
	v, err := s.current(ctx)
	if err != nil {
		return nginxguard.SetupStatus{Message: err.Error()}
	}
	return s.controller.Setup(ctx, v.platform.ControlConfig())
}

// NginxInitialization reads the existing CMS settings without creating tables
// or writing SQLite files as root. It is used only by the explicit init CLI.
func NginxInitialization(ctx context.Context, database *sql.DB) (nginxguard.Config, nginxguard.Snapshot, error) {
	service := &Service{database: database}
	v, snapshot, err := service.desired(ctx)
	if err != nil {
		return nginxguard.Config{}, nginxguard.Snapshot{}, err
	}
	// Root setup needs domain ownership even when synchronization is disabled.
	// Keep discovery data but do not activate disabled rules or scan logging.
	if !v.platform.NginxEnabled {
		snapshot.Tenants = append([]nginxguard.Tenant{}, v.snapshot.Tenants...)
		for i := range snapshot.Tenants {
			snapshot.Tenants[i].Paths = nil
		}
	}
	return v.platform.ControlConfig(), snapshot, nil
}
