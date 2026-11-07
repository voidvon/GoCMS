package site

import (
	"context"
	"strings"
	"time"

	"gocms/internal/membermail"
)

// Durable, bounded, at-least-once notification delivery. SMTP failure never
// rolls back a shipment. Disabled mail skips queued messages without a backlog.
func (s *Server) shipmentNotices(ctx context.Context) {
	rows, err := s.database.QueryContext(ctx, `SELECT p.id,p.order_id,o.site_id,o.email,o.language FROM gocms_commerce_shipment p JOIN gocms_commerce_order o ON o.id=p.order_id WHERE p.notice_status IN ('pending','sending') AND p.notice_next<=? ORDER BY p.created_at LIMIT 2`, time.Now().Unix())
	if err != nil {
		return
	}
	type task struct {
		id, order, email, language string
		site                       int64
	}
	tasks := []task{}
	for rows.Next() {
		var t task
		if err = rows.Scan(&t.id, &t.order, &t.site, &t.email, &t.language); err != nil {
			rows.Close()
			return
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for _, t := range tasks {
		if ctx.Err() != nil {
			return
		}
		c, configErr := membermail.Load(ctx, s.database)
		if configErr != nil {
			return
		}
		if !c.Enabled {
			_, _ = s.database.ExecContext(ctx, `UPDATE gocms_commerce_shipment SET notice_status='skipped' WHERE id=? AND notice_status IN ('pending','sending') AND notice_next<=?`, t.id, time.Now().Unix())
			continue
		}
		if membermail.Validate(c) != nil {
			continue
		}
		secret := ""
		if c.Encrypted != "" {
			s.oauthMu.RLock()
			secret, configErr = s.oauthVault.Open(membermail.SecretScope, c.Encrypted)
			s.oauthMu.RUnlock()
			if configErr != nil {
				continue
			}
		}

		r, e := s.database.ExecContext(ctx, `UPDATE gocms_commerce_shipment SET notice_status='sending',notice_next=?,notice_attempts=notice_attempts+1 WHERE id=? AND notice_status IN ('pending','sending') AND notice_next<=?`, time.Now().Add(5*time.Minute).Unix(), t.id, time.Now().Unix())
		if e != nil {
			continue
		}
		if n, _ := r.RowsAffected(); n != 1 {
			continue
		}
		packages, e := s.commerce.Shipments(ctx, t.site, t.order)
		if e != nil {
			continue
		}
		for _, p := range packages {
			if p.ID != t.id {
				continue
			}
			subject, body := membermail.ShipmentMessage(t.language, t.order, strings.Join([]string{p.Carrier, p.TrackingNumber, p.TrackingURL, p.Note}, "\n"))
			send := s.mailSend
			if send == nil {
				send = membermail.Send
			}
			e = send(ctx, c, secret, t.email, subject, body)
			state := "sent"
			if e != nil {
				state = "pending"
			}
			_, _ = s.database.ExecContext(ctx, `UPDATE gocms_commerce_shipment SET notice_status=CASE WHEN ?='pending' AND notice_attempts>=5 THEN 'failed' ELSE ? END WHERE id=?`, state, state, t.id)
			break
		}
	}
}
