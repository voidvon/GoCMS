package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"syscall"
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

func agentConnectionError(socket string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("当前使用独立代理模式，未找到 Nginx 同步代理的 Socket（%s）。无需安装代理时，请在“Nginx 接入”选择“GoCMS 直接管理”，保存后重新同步；继续使用代理模式时，请检查代理服务与 Socket 路径，尚未安装时再下载接入包", socket)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("GoCMS 无权访问 Nginx 同步代理（%s）：请确认接入参数中的系统运行用户与 GoCMS 实际进程用户一致，再重新安装接入包", socket)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("Nginx 同步代理未运行或未监听（%s）：请检查 gocms-security-agent 服务状态", socket)
	default:
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return errors.New("Nginx 同步代理响应超时，请检查代理服务及 Nginx 配置检查、重载日志")
		}
		return fmt.Errorf("无法连接本机 Nginx 同步代理（%s）：%w", socket, err)
	}
}

func agentCall(ctx context.Context, socket, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return agentConnectionError(socket, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error != "" {
			return fmt.Errorf("Nginx 同步失败：%s", e.Error)
		}
		return fmt.Errorf("Nginx 同步器返回 %d", res.StatusCode)
	}
	return json.Unmarshal(data, output)
}

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
	var result nginxguard.Result
	if v.platform.NginxMode == "direct" {
		result, err = s.directController(v.platform).Apply(want)
	} else {
		err = agentCall(ctx, v.platform.SocketPath, http.MethodPost, "/apply", want, &result)
	}
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

// Caller holds syncMu; Direct serializes its file and log operations itself.
func (s *Service) directController(p Platform) *nginxguard.Direct {
	c := nginxguard.Config{SocketPath: p.SocketPath, NginxBinary: p.NginxBinary, NginxConfig: p.NginxConfig, ServerConfig: p.ServerConfig, AgentUser: p.AgentUser}
	if s.direct == nil || s.directConfig != c {
		s.direct, s.directConfig = nginxguard.NewDirect(c), c
	}
	return s.direct
}

func (s *Service) Preview(ctx context.Context) (string, string, error) {
	v, want, err := s.desired(ctx)
	if err != nil {
		return "", "", err
	}
	if v.platform.NginxMode == "direct" {
		return nginxguard.SingleFileFragments(want, time.Now(), "/var/log/gocms-security/probes.log")
	}
	return nginxguard.Render(want, time.Now(), "/var/log/gocms-security/probes.log")
}

// Pull Nginx-only probes as well: Nginx may reject .env/.git before they reach
// Go. The server logs only requests that never selected an upstream.
func (s *Service) pull(ctx context.Context, p Platform) error {
	var cursor string
	err := s.database.QueryRowContext(ctx, `SELECT value FROM gocms_security_state WHERE key='cursor'`).Scan(&cursor)
	if err != nil {
		cursor = ""
	}
	var batch nginxguard.LogBatch
	if p.NginxMode == "direct" {
		s.syncMu.Lock()
		controller := s.directController(p)
		s.syncMu.Unlock()
		batch, err = controller.Events(cursor)
	} else {
		err = agentCall(ctx, p.SocketPath, http.MethodPost, "/events", map[string]string{"cursor": cursor}, &batch)
	}
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
