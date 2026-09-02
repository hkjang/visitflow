package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// requeueNotificationSQL returns a failed or cancelled message to the queue.
// Attempts reset so the delivery worker's retry budget applies again, and the
// claim fields clear so no stale worker can report a result for it.
const requeueNotificationSQL = `UPDATE notifications
	SET status='queued',attempts=0,error=NULL,next_attempt_at=now(),claimed_at=NULL,claim_token=NULL,sent_at=NULL,provider_message_id=NULL
	WHERE id=ANY($1::text[]) AND status IN ('failed','cancelled')
	AND (api_config_id IS NULL OR EXISTS(SELECT 1 FROM notification_api_configs api WHERE api.id=notifications.api_config_id AND api.enabled))
	AND (rule_id IS NULL OR EXISTS(SELECT 1 FROM notification_rules rule WHERE rule.id=notifications.rule_id AND rule.enabled))`

func (s *Server) notificationBlockedReason(ctx context.Context, id string) string {
	var status string
	var apiEnabled, ruleEnabled *bool
	err := s.db.QueryRow(ctx, `SELECT n.status,api.enabled,rule.enabled FROM notifications n
		LEFT JOIN notification_api_configs api ON api.id=n.api_config_id
		LEFT JOIN notification_rules rule ON rule.id=n.rule_id WHERE n.id=$1`, id).Scan(&status, &apiEnabled, &ruleEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return "알림을 찾을 수 없습니다"
	}
	if err != nil {
		return "알림 상태를 확인하지 못했습니다"
	}
	if apiEnabled != nil && !*apiEnabled {
		return "연결된 문자 API가 비활성화되어 있어 재시도할 수 없습니다"
	}
	if ruleEnabled != nil && !*ruleEnabled {
		return "연결된 발송 규칙이 비활성화되어 있어 재시도할 수 없습니다"
	}
	return "실패 또는 취소된 알림만 재시도할 수 있습니다 (현재 " + status + ")"
}

func (s *Server) retryNotification(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "notificationID")
	tag, err := s.db.Exec(r.Context(), requeueNotificationSQL, []string{id})
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "notification_not_retryable", s.notificationBlockedReason(r.Context(), id))
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification.retry", "notification", id, r.RemoteAddr, nil)
	writeJSON(w, http.StatusOK, map[string]int{"queued": 1})
}

func (s *Server) retryFailedNotifications(w http.ResponseWriter, r *http.Request) {
	ids := []string{}
	rows, err := s.db.Query(r.Context(), `SELECT id FROM notifications WHERE status='failed' ORDER BY created_at LIMIT 500`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"queued": 0})
		return
	}
	tag, err := s.db.Exec(r.Context(), requeueNotificationSQL, ids)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification.retry_failed", "notification", "", r.RemoteAddr, map[string]any{"selected": len(ids), "queued": tag.RowsAffected()})
	writeJSON(w, http.StatusOK, map[string]int64{"selected": int64(len(ids)), "queued": tag.RowsAffected()})
}

func (s *Server) cancelNotification(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "notificationID")
	tag, err := s.db.Exec(r.Context(), `UPDATE notifications SET status='cancelled',error='관리자가 발송을 취소했습니다',claimed_at=NULL,claim_token=NULL
		WHERE id=$1 AND status IN ('queued','failed')`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "notification_not_cancellable", "대기 또는 실패 상태의 알림만 취소할 수 있습니다")
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification.cancel", "notification", id, r.RemoteAddr, nil)
	w.WriteHeader(http.StatusNoContent)
}
