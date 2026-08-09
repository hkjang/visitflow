package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) RunBackground(ctx context.Context) {
	notifications := time.NewTicker(10 * time.Second)
	maintenance := time.NewTicker(10 * time.Minute)
	privacy := time.NewTicker(6 * time.Hour)
	defer notifications.Stop()
	defer maintenance.Stop()
	defer privacy.Stop()
	s.processNotifications(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-notifications.C:
			s.processNotifications(ctx)
		case <-maintenance.C:
			s.runVisitMaintenance(ctx)
		case <-privacy.C:
			s.runPrivacyMaintenance(ctx)
		}
	}
}

func (s *Server) processNotifications(ctx context.Context) {
	rows, err := s.db.Query(ctx, `UPDATE notifications SET status='sending',attempts=attempts+1 WHERE id IN (SELECT id FROM notifications WHERE status IN ('queued','failed') AND next_attempt_at<=now() AND attempts<5 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 20) RETURNING id,recipient_encrypted,body_encrypted,channel`)
	if err != nil {
		s.logger.Error("notification queue claim failed", "error", err)
		return
	}
	type item struct{ id, recipient, body, channel string }
	items := []item{}
	for rows.Next() {
		var x item
		if rows.Scan(&x.id, &x.recipient, &x.body, &x.channel) == nil {
			x.recipient = s.decryptOptional(x.recipient)
			x.body = s.decryptOptional(x.body)
			items = append(items, x)
		}
	}
	rows.Close()
	provider, _ := s.getSetting(ctx, "notification.provider")
	for _, item := range items {
		if provider == "log" || provider == "" {
			s.logger.Info("notification logged", "id", item.id, "channel", item.channel, "recipient", maskPhone(item.recipient))
			_, _ = s.db.Exec(ctx, `UPDATE notifications SET status='logged',sent_at=now(),error=NULL WHERE id=$1`, item.id)
			continue
		}
		if provider != "webhook" {
			s.failNotification(ctx, item.id, "지원하지 않는 알림 provider: "+provider)
			continue
		}
		endpoint, _ := s.getSetting(ctx, "notification.webhook_url")
		auth, _ := s.getSetting(ctx, "notification.auth_header")
		payload, _ := json.Marshal(map[string]string{"recipient": item.recipient, "message": item.body, "channel": item.channel, "idempotencyKey": item.id})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if auth != "" {
				req.Header.Set("Authorization", auth)
			}
			client := &http.Client{Timeout: 10 * time.Second}
			var response *http.Response
			response, err = client.Do(req)
			if response != nil {
				_ = response.Body.Close()
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					err = fmt.Errorf("webhook status %d", response.StatusCode)
				}
			}
		}
		if err != nil {
			s.failNotification(ctx, item.id, err.Error())
			continue
		}
		_, _ = s.db.Exec(ctx, `UPDATE notifications SET status='sent',sent_at=now(),error=NULL WHERE id=$1`, item.id)
	}
}

func (s *Server) failNotification(ctx context.Context, id, message string) {
	_, _ = s.db.Exec(ctx, `UPDATE notifications SET status='failed',error=$2,next_attempt_at=now()+(LEAST(attempts,5)*interval '5 minutes') WHERE id=$1`, id, message)
}

func (s *Server) runVisitMaintenance(ctx context.Context) {
	lateMinutes, _ := strconv.Atoi(settingOr(s, ctx, "visit.late_grace_minutes", "120"))
	if lateMinutes < 0 || lateMinutes > 1440 {
		lateMinutes = 120
	}
	_, err := s.db.Exec(ctx, `UPDATE visitor_visits vv SET status='NO_SHOW' FROM visits v WHERE vv.visit_id=v.id AND vv.status='SCHEDULED' AND v.end_at+($1::int*interval '1 minute')<now()`, lateMinutes)
	if err != nil {
		s.logger.Error("no-show maintenance failed", "error", err)
	}
	hour, _ := strconv.Atoi(settingOr(s, ctx, "visit.auto_checkout_hour", "23"))
	if hour >= 0 && hour <= 23 && time.Now().Hour() >= hour {
		_, err = s.db.Exec(ctx, `WITH ended AS (UPDATE visitor_visits SET status='CHECKED_OUT',checked_out_at=now() WHERE status='CHECKED_IN' AND checked_in_at::date<CURRENT_DATE+1 RETURNING visit_id,id) INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,method,details) SELECT visit_id,id,'CHECKED_OUT','automatic','{"reason":"policy cutoff"}'::jsonb FROM ended`)
		if err == nil {
			_, _ = s.db.Exec(ctx, `UPDATE visits SET status='CHECKED_OUT',updated_at=now() WHERE status='CHECKED_IN' AND NOT EXISTS(SELECT 1 FROM visitor_visits vv WHERE vv.visit_id=visits.id AND vv.status='CHECKED_IN')`)
		}
	}
	_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at<now(); DELETE FROM oidc_states WHERE expires_at<now();`)
}

func (s *Server) runPrivacyMaintenance(ctx context.Context) {
	destroyDays, _ := strconv.Atoi(settingOr(s, ctx, "privacy.destroy_after_days", "365"))
	if destroyDays < 1 {
		return
	}
	rows, err := s.db.Query(ctx, `SELECT p.id FROM visitors p WHERE p.erased_at IS NULL AND NOT EXISTS(SELECT 1 FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE vv.visitor_id=p.id AND v.end_at>=now()-($1::int*interval '1 day')) LIMIT 500`, destroyDays)
	if err != nil {
		s.logger.Error("privacy selection failed", "error", err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	erased, err := s.keys.Encrypt("파기됨")
	if err == nil {
		for _, id := range ids {
			_, _ = s.db.Exec(ctx, `UPDATE visitors SET name_encrypted=$2,phone_encrypted=$2,phone_hash=NULL,email_encrypted=NULL,vehicle_encrypted=NULL,company=NULL,title=NULL,erased_at=now(),updated_at=now() WHERE id=$1`, id, erased)
		}
	}
	auditDays, _ := strconv.Atoi(settingOr(s, ctx, "privacy.audit_retention_days", "730"))
	if auditDays > 0 {
		_, _ = s.db.Exec(ctx, `DELETE FROM audit_logs WHERE created_at<now()-($1::int*interval '1 day')`, auditDays)
	}
}
