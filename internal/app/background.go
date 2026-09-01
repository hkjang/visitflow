package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Server) RunBackground(ctx context.Context) {
	notifications := time.NewTicker(10 * time.Second)
	maintenance := time.NewTicker(10 * time.Minute)
	privacy := time.NewTicker(6 * time.Hour)
	defer notifications.Stop()
	defer maintenance.Stop()
	defer privacy.Stop()
	s.processNotifications(ctx)
	s.runVisitMaintenance(ctx)
	s.runPrivacyMaintenance(ctx)
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
	_, err := s.db.Exec(ctx, `UPDATE notifications
		SET status='failed',claimed_at=NULL,claim_token=NULL,error='최대 재시도 횟수에 도달했습니다'
		WHERE status='sending' AND attempts>=5 AND (claimed_at IS NULL OR claimed_at<now()-interval '2 minutes')`)
	if err != nil {
		s.logger.Error("expired notification claim cleanup failed", "error", err)
		return
	}
	// Claim immediately before processing. This avoids expiring later entries in
	// a batch while an earlier provider call is still using its timeout budget.
	for processed := 0; processed < 20; processed++ {
		item, found, claimErr := s.claimNotification(ctx)
		if claimErr != nil {
			s.logger.Error("notification queue claim failed", "error", claimErr)
			return
		}
		if !found {
			return
		}
		s.processClaimedNotification(ctx, item)
	}
}

type notificationQueueItem struct {
	id, recipientEncrypted, bodyEncrypted, channel, apiConfigID, metadataEncrypted, claimToken string
}

func (s *Server) claimNotification(ctx context.Context) (notificationQueueItem, bool, error) {
	item := notificationQueueItem{claimToken: newID()}
	err := s.db.QueryRow(ctx, `WITH candidate AS (
		SELECT n.id FROM notifications n
		LEFT JOIN notification_api_configs api ON api.id=n.api_config_id
		LEFT JOIN notification_rules rule ON rule.id=n.rule_id
		WHERE n.attempts<5
		AND (n.api_config_id IS NULL OR COALESCE(api.enabled,false))
		AND (n.rule_id IS NULL OR COALESCE(rule.enabled,false))
		AND (
			(n.status IN ('queued','failed') AND n.next_attempt_at<=now()) OR
			(n.status='sending' AND (n.claimed_at IS NULL OR n.claimed_at<now()-interval '2 minutes'))
		)
		ORDER BY n.next_attempt_at,n.created_at
		FOR UPDATE OF n SKIP LOCKED LIMIT 1
	) UPDATE notifications n
	SET status='sending',attempts=n.attempts+1,claimed_at=now(),claim_token=$1
	FROM candidate WHERE n.id=candidate.id
	RETURNING n.id,n.recipient_encrypted,n.body_encrypted,n.channel,COALESCE(n.api_config_id,''),n.metadata_encrypted`, item.claimToken).
		Scan(&item.id, &item.recipientEncrypted, &item.bodyEncrypted, &item.channel, &item.apiConfigID, &item.metadataEncrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return notificationQueueItem{}, false, nil
	}
	return item, err == nil, err
}

func (s *Server) processClaimedNotification(ctx context.Context, item notificationQueueItem) {
	recipient, err := s.keys.Decrypt(item.recipientEncrypted)
	if err != nil {
		s.failNotification(ctx, item.id, item.claimToken, "알림 수신 번호를 복호화하지 못했습니다")
		return
	}
	body, err := s.keys.Decrypt(item.bodyEncrypted)
	if err != nil {
		s.failNotification(ctx, item.id, item.claimToken, "알림 본문을 복호화하지 못했습니다")
		return
	}

	if item.apiConfigID != "" {
		metadata, metadataErr := parseNotificationMetadata(s, item.metadataEncrypted)
		if metadataErr != nil {
			s.failNotification(ctx, item.id, item.claimToken, "알림 metadata를 복호화하지 못했습니다")
			return
		}
		config, configErr := s.loadNotificationAPI(ctx, item.apiConfigID)
		if errors.Is(configErr, errNotificationAPIDisabled) {
			s.cancelClaimedNotification(ctx, item.id, item.claimToken, "문자 API가 비활성화되어 발송을 취소했습니다")
			return
		}
		if configErr != nil {
			s.failNotification(ctx, item.id, item.claimToken, truncateNotificationError(configErr.Error(), 500))
			return
		}
		if config.Channel != item.channel {
			s.failNotification(ctx, item.id, item.claimToken, "발송 규칙과 문자 API의 채널이 일치하지 않습니다")
			return
		}
		if !notificationValuesUseIdempotency(config.Headers, config.Parameters) {
			s.cancelClaimedNotification(ctx, item.id, item.claimToken, "문자 API에 idempotencyKey 또는 notificationId 변수가 없습니다")
			return
		}
		s.dispatchClaimedNotification(ctx, item, func() (string, error) {
			return sendConfiguredNotification(ctx, config, claimedNotification{ID: item.id, Recipient: recipient, Message: body, Channel: item.channel, APIConfigID: item.apiConfigID, Metadata: metadata})
		})
		return
	}

	provider, providerErr := s.getSetting(ctx, "notification.provider")
	if providerErr != nil {
		s.failNotification(ctx, item.id, item.claimToken, "기존 알림 provider 설정을 불러오지 못했습니다")
		return
	}
	if provider == "log" || provider == "" {
		s.logger.Info("notification logged", "id", item.id, "channel", item.channel, "recipient", maskPhone(recipient))
		s.saveNotificationResult(ctx, `UPDATE notifications SET status='logged',sent_at=now(),error=NULL,claimed_at=NULL,claim_token=NULL WHERE id=$1 AND status='sending' AND claim_token=$2`, item.id, item.claimToken)
		return
	}
	if provider != "webhook" {
		s.failNotification(ctx, item.id, item.claimToken, "지원하지 않는 알림 provider: "+provider)
		return
	}
	endpoint, _ := s.getSetting(ctx, "notification.webhook_url")
	auth, _ := s.getSetting(ctx, "notification.auth_header")
	payload, _ := json.Marshal(map[string]string{"recipient": recipient, "message": body, "channel": item.channel, "idempotencyKey": item.id})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		s.dispatchClaimedNotification(ctx, item, func() (string, error) {
			client := &http.Client{Timeout: 10 * time.Second}
			response, sendErr := client.Do(req)
			if response != nil {
				_ = response.Body.Close()
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					sendErr = fmt.Errorf("webhook status %d", response.StatusCode)
				}
			}
			return "", sendErr
		})
		return
	}
	if err != nil {
		s.failNotification(ctx, item.id, item.claimToken, err.Error())
		return
	}
}

func sendConfiguredNotification(ctx context.Context, config notificationAPIConfig, item claimedNotification) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := buildNotificationRequest(timeoutCtx, config, item)
	if err != nil {
		return "", errors.New("문자 API 요청을 구성하지 못했습니다")
	}
	client := &http.Client{
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("문자 API 호출 시간이 초과되었습니다")
		}
		return "", errors.New("문자 API를 호출하지 못했습니다")
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if readErr != nil {
		return "", errors.New("문자 API 응답을 읽지 못했습니다")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("문자 API 응답 상태 %d", response.StatusCode)
	}
	return providerMessageID(response.Header, body), nil
}

const notificationDispatchLockSQL = `SELECT id FROM notifications WHERE id=$1 AND status='sending' AND claim_token=$2 FOR UPDATE`

// dispatchClaimedNotification holds the notification row lock for the whole
// provider call. That lock is the linearization boundary between dispatch and
// cancellation: a concurrent cancel/reissue/rule/API update waits, then its
// status predicate is re-evaluated after this transaction records sent/failed.
func (s *Server) dispatchClaimedNotification(parent context.Context, item notificationQueueItem, send func() (string, error)) {
	beginCtx, cancelBegin := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	tx, err := s.db.Begin(beginCtx)
	cancelBegin()
	if err != nil {
		s.logger.Error("notification dispatch transaction failed", "id", item.id, "error", err)
		return
	}
	defer tx.Rollback(context.Background())

	lockCtx, cancelLock := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	var lockedID string
	err = tx.QueryRow(lockCtx, notificationDispatchLockSQL, item.id, item.claimToken).Scan(&lockedID)
	cancelLock()
	if errors.Is(err, pgx.ErrNoRows) {
		s.logger.Info("notification dispatch skipped because claim is no longer owned", "id", item.id)
		return
	}
	if err != nil {
		s.logger.Error("notification dispatch lock failed", "id", item.id, "error", err)
		return
	}

	messageID, sendErr := send()
	resultCtx, cancelResult := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	var tag pgconn.CommandTag
	if sendErr != nil {
		tag, err = tx.Exec(resultCtx, `UPDATE notifications
			SET status='failed',error=$2,next_attempt_at=now()+(LEAST(attempts,5)*interval '5 minutes'),claimed_at=NULL,claim_token=NULL
			WHERE id=$1 AND status='sending' AND claim_token=$3`, item.id, truncateNotificationError(sendErr.Error(), 500), item.claimToken)
	} else {
		tag, err = tx.Exec(resultCtx, `UPDATE notifications
			SET status='sent',sent_at=now(),error=NULL,provider_message_id=NULLIF($2,''),claimed_at=NULL,claim_token=NULL
			WHERE id=$1 AND status='sending' AND claim_token=$3`, item.id, messageID, item.claimToken)
	}
	cancelResult()
	if err != nil {
		s.logger.Error("notification dispatch result update failed", "id", item.id, "error", err)
		return
	}
	if tag.RowsAffected() != 1 {
		s.logger.Warn("notification dispatch result ignored because claim is no longer owned", "id", item.id, "rowsAffected", tag.RowsAffected())
		return
	}

	commitCtx, cancelCommit := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	err = tx.Commit(commitCtx)
	cancelCommit()
	if err != nil {
		s.logger.Error("notification dispatch commit failed", "id", item.id, "error", err)
	}
}

func (s *Server) failNotification(ctx context.Context, id, claimToken, message string) {
	s.saveNotificationResult(ctx, `UPDATE notifications SET status='failed',error=$2,next_attempt_at=now()+(LEAST(attempts,5)*interval '5 minutes'),claimed_at=NULL,claim_token=NULL WHERE id=$1 AND status='sending' AND claim_token=$3`, id, truncateNotificationError(message, 500), claimToken)
}

func (s *Server) cancelClaimedNotification(ctx context.Context, id, claimToken, message string) {
	s.saveNotificationResult(ctx, `UPDATE notifications SET status='cancelled',attempts=GREATEST(attempts-1,0),error=$2,claimed_at=NULL,claim_token=NULL WHERE id=$1 AND status='sending' AND claim_token=$3`, id, truncateNotificationError(message, 500), claimToken)
}

func (s *Server) saveNotificationResult(parent context.Context, query string, args ...any) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	tag, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		s.logger.Error("notification result update failed", "error", err)
		return false
	}
	if tag.RowsAffected() != 1 {
		s.logger.Warn("notification result ignored because claim is no longer owned", "rowsAffected", tag.RowsAffected())
		return false
	}
	return true
}

func (s *Server) runVisitMaintenance(ctx context.Context) {
	lateMinutes, _ := strconv.Atoi(settingOr(s, ctx, "visit.late_grace_minutes", "120"))
	if lateMinutes < 0 || lateMinutes > 1440 {
		lateMinutes = 120
	}
	_, err := s.db.Exec(ctx, `WITH missed AS (
		UPDATE visitor_visits vv SET status='NO_SHOW' FROM visits v
		WHERE vv.visit_id=v.id AND vv.status='SCHEDULED' AND v.end_at+($1::int*interval '1 minute')<now()
		RETURNING vv.visit_id,vv.id
	) INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,method,details)
	SELECT visit_id,id,'NO_SHOW','automatic','{"reason":"late grace expired"}'::jsonb FROM missed`, lateMinutes)
	if err != nil {
		s.logger.Error("no-show maintenance failed", "error", err)
	} else {
		_, _ = s.db.Exec(ctx, `UPDATE visits SET status='NO_SHOW',updated_at=now() WHERE status='SCHEDULED' AND NOT EXISTS(SELECT 1 FROM visitor_visits vv WHERE vv.visit_id=visits.id AND vv.status<>'NO_SHOW')`)
	}
	hour, _ := strconv.Atoi(settingOr(s, ctx, "visit.auto_checkout_hour", "23"))
	if hour >= 0 && hour <= 23 && time.Now().Hour() >= hour {
		err = s.runAutomaticCheckouts(ctx)
		if err != nil {
			s.logger.Error("automatic checkout failed", "error", err)
		}
	}
	_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at<now(); DELETE FROM oidc_states WHERE expires_at<now();`)
}

type automaticCheckoutItem struct {
	visitID, participantID string
	checkedOutAt           time.Time
}

func (s *Server) runAutomaticCheckouts(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ended := []automaticCheckoutItem{}
	rows, err := tx.Query(ctx, `UPDATE visitor_visits
		SET status='CHECKED_OUT',checked_out_at=now()
		WHERE status='CHECKED_IN' AND checked_in_at::date<CURRENT_DATE+1
		RETURNING visit_id,id,checked_out_at`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item automaticCheckoutItem
		if err := rows.Scan(&item.visitID, &item.participantID, &item.checkedOutAt); err != nil {
			rows.Close()
			return err
		}
		ended = append(ended, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range ended {
		if _, err := tx.Exec(ctx, `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,method,details) VALUES($1,$2,'CHECKED_OUT','automatic','{"reason":"policy cutoff"}'::jsonb)`, item.visitID, item.participantID); err != nil {
			return err
		}
	}
	if len(ended) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE visits SET status=CASE
			WHEN EXISTS(SELECT 1 FROM visitor_visits vv WHERE vv.visit_id=visits.id AND vv.status='CHECKED_IN') THEN 'CHECKED_IN'
			WHEN EXISTS(SELECT 1 FROM visitor_visits vv WHERE vv.visit_id=visits.id AND vv.status IN ('SCHEDULED','ARRIVED')) THEN 'SCHEDULED'
			ELSE 'CHECKED_OUT' END,updated_at=now()
			WHERE id=ANY($1::text[])`, checkedOutVisitIDs(ended)); err != nil {
			return err
		}
	}
	for _, item := range ended {
		if err := s.queueNotificationEventTx(ctx, tx, item.visitID, item.participantID, "checked_out", item.checkedOutAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func checkedOutVisitIDs(items []automaticCheckoutItem) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item.visitID] {
			seen[item.visitID] = true
			result = append(result, item.visitID)
		}
	}
	return result
}

func (s *Server) runPrivacyMaintenance(ctx context.Context) {
	maskDays, _ := strconv.Atoi(settingOr(s, ctx, "privacy.mask_after_days", "90"))
	if maskDays > 0 {
		tag, err := s.db.Exec(ctx, `UPDATE visitors p SET masked_at=now(),updated_at=now() WHERE p.erased_at IS NULL AND p.masked_at IS NULL AND NOT EXISTS(SELECT 1 FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE vv.visitor_id=p.id AND v.end_at>=now()-($1::int*interval '1 day'))`, maskDays)
		if err != nil {
			s.logger.Error("privacy masking failed", "error", err)
		} else if tag.RowsAffected() > 0 {
			s.audit(ctx, "", "privacy.mask", "visitor", "", "background", map[string]any{"count": tag.RowsAffected(), "afterDays": maskDays})
		}
	}
	destroyDays, _ := strconv.Atoi(settingOr(s, ctx, "privacy.destroy_after_days", "365"))
	if destroyDays < 1 {
		return
	}
	frequentTag, frequentErr := s.db.Exec(ctx, `DELETE FROM frequent_visitors WHERE last_used_at<now()-($1::int*interval '1 day')`, destroyDays)
	if frequentErr != nil {
		s.logger.Error("frequent visitor privacy destruction failed", "error", frequentErr)
	} else if frequentTag.RowsAffected() > 0 {
		s.audit(ctx, "", "privacy.destroy", "frequent_visitor", "", "background", map[string]any{"count": frequentTag.RowsAffected(), "afterDays": destroyDays})
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
			_, _ = s.db.Exec(ctx, `UPDATE visitors SET name_encrypted=$2,name_hash=NULL,phone_encrypted=$2,phone_hash=NULL,email_encrypted=NULL,vehicle_encrypted=NULL,company=NULL,title=NULL,masked_at=COALESCE(masked_at,now()),erased_at=now(),updated_at=now() WHERE id=$1`, id, erased)
		}
		if len(ids) > 0 {
			s.audit(ctx, "", "privacy.destroy", "visitor", "", "background", map[string]any{"count": len(ids), "afterDays": destroyDays})
		}
	}
	auditDays, _ := strconv.Atoi(settingOr(s, ctx, "privacy.audit_retention_days", "730"))
	if auditDays > 0 {
		_, _ = s.db.Exec(ctx, `DELETE FROM audit_logs WHERE created_at<now()-($1::int*interval '1 day')`, auditDays)
	}
}
