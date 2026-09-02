package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// cancelParticipant removes one visitor from a group visit without touching the
// others. When the last open participant leaves, the visit itself is cancelled.
func (s *Server) cancelParticipant(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	participantID := chi.URLParam(r, "visitorVisitID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var visitID, hostID, visitStatus, participantStatus string
	err = tx.QueryRow(r.Context(), `SELECT v.id,v.host_user_id,v.status,vv.status FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE vv.id=$1 FOR UPDATE OF vv,v`, participantID).
		Scan(&visitID, &hostID, &visitStatus, &participantStatus)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !s.actsForHost(r.Context(), u, hostID) && !u.IsAdmin() && u.Role != RoleSecurity {
		writeError(w, http.StatusForbidden, "forbidden", "방문자를 취소할 권한이 없습니다")
		return
	}
	if qrParticipantStatusTerminal(participantStatus) || participantStatus == "CHECKED_IN" || qrParticipantStatusTerminal(visitStatus) {
		writeError(w, http.StatusConflict, "participant_not_cancellable", "이미 입실했거나 종료된 방문자는 취소할 수 없습니다")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE visitor_visits SET status='CANCELLED' WHERE id=$1`, participantID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE qr_tokens SET revoked_at=now() WHERE visitor_visit_id=$1 AND revoked_at IS NULL`, participantID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE notifications SET status='cancelled',error=NULL,claimed_at=NULL,claim_token=NULL,
		attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END
		WHERE visitor_visit_id=$1 AND status IN ('queued','failed','sending')`, participantID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE registration_invitations SET revoked_at=now() WHERE visitor_visit_id=$1 AND completed_at IS NULL AND revoked_at IS NULL`, participantID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,actor_user_id,method) VALUES($1,$2,'PARTICIPANT_CANCELLED',NULLIF($3,''),'host')`, visitID, participantID, u.ID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = s.queueNotificationEventTx(r.Context(), tx, visitID, participantID, "visit_cancelled", time.Now()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	var remaining int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM visitor_visits WHERE visit_id=$1 AND status NOT IN ('CANCELLED','REJECTED')`, visitID).Scan(&remaining); err != nil {
		notFoundOrServer(w, err)
		return
	}
	visitCancelled := false
	if remaining == 0 {
		if visitCancelled, err = s.cancelVisitTx(r.Context(), tx, visitID, u); err != nil {
			notFoundOrServer(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit.participant_cancel", "visitor_visit", participantID, clientIP(r), map[string]any{"visitId": visitID, "visitCancelled": visitCancelled})
	s.publishLobbyEvent("visit.updated")
	writeJSON(w, http.StatusOK, map[string]any{"visitCancelled": visitCancelled, "remaining": remaining})
}

// cancelSeries cancels this occurrence and every later occurrence of the same
// weekly series, which is how a recurring engagement actually ends.
func (s *Server) cancelSeries(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "visitID")
	var seriesID string
	var startAt time.Time
	if err := s.db.QueryRow(r.Context(), `SELECT COALESCE(recurrence->>'seriesId',''),start_at FROM visits WHERE id=$1`, id).Scan(&seriesID, &startAt); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if seriesID == "" {
		writeError(w, http.StatusBadRequest, "not_a_series", "반복 일정이 아닌 방문입니다")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT id FROM visits WHERE recurrence->>'seriesId'=$1 AND start_at>=$2 AND status NOT IN ('CHECKED_OUT','CANCELLED','REJECTED') ORDER BY start_at`, seriesID, startAt)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var visitID string
		if rows.Scan(&visitID) == nil {
			ids = append(ids, visitID)
		}
	}
	rows.Close()
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	cancelled := 0
	for _, visitID := range ids {
		done, cancelErr := s.cancelVisitTx(r.Context(), tx, visitID, u)
		if cancelErr != nil {
			notFoundOrServer(w, cancelErr)
			return
		}
		if done {
			cancelled++
		}
	}
	if cancelled == 0 {
		writeError(w, http.StatusConflict, "cannot_cancel", "취소할 수 있는 반복 일정이 없거나 권한이 없습니다")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit.cancel_series", "visit", id, clientIP(r), map[string]any{"seriesId": seriesID, "cancelled": cancelled})
	s.publishLobbyEvent("visit.cancelled")
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

// manualCheckIn admits a scheduled visitor who cannot present a QR code. The
// operator records how identity was confirmed, the QR is consumed so it cannot
// be replayed later, and the event carries the reason for the audit trail.
func (s *Server) manualCheckIn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		VisitorVisitID string `json:"visitorVisitId"`
		LobbyID        string `json:"lobbyId"`
		BadgeNo        string `json:"badgeNo"`
		Reason         string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.VisitorVisitID == "" || in.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason_required", "방문자와 신분 확인 방법(사유)을 입력하세요")
		return
	}
	u, _ := userFrom(r)
	if _, ok := kioskFrom(r); ok {
		writeError(w, http.StatusForbidden, "staff_required", "QR 없는 직접 체크인은 로비 담당자만 처리할 수 있습니다")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var visitID, visitStatus, participantStatus, siteID string
	var startAt, endAt time.Time
	err = tx.QueryRow(r.Context(), `SELECT v.id,v.status,vv.status,v.site_id,v.start_at,v.end_at FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE vv.id=$1 FOR UPDATE OF vv,v`, in.VisitorVisitID).
		Scan(&visitID, &visitStatus, &participantStatus, &siteID, &startAt, &endAt)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !siteAllowed(u, siteID) {
		writeError(w, http.StatusForbidden, "site_scope_forbidden", "담당 사업장이 아닌 방문입니다")
		return
	}
	if visitStatus == "CANCELLED" || visitStatus == "REJECTED" || visitStatus == "PENDING_APPROVAL" || participantStatus != "SCHEDULED" && participantStatus != "ARRIVED" {
		writeError(w, http.StatusConflict, "not_checkin_ready", "예정 상태의 방문자만 직접 체크인할 수 있습니다")
		return
	}
	earlyMinutes, _ := strconv.Atoi(settingOr(s, r.Context(), "visit.early_checkin_minutes", "60"))
	lateMinutes, _ := strconv.Atoi(settingOr(s, r.Context(), "visit.late_grace_minutes", "120"))
	now := time.Now()
	if now.Before(startAt.Add(-time.Duration(earlyMinutes)*time.Minute)) || now.After(endAt.Add(time.Duration(lateMinutes)*time.Minute)) {
		writeError(w, http.StatusConflict, "outside_window", "방문 가능 시간대가 아닙니다")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE qr_tokens SET used_at=COALESCE(used_at,now()) WHERE visitor_visit_id=$1 AND revoked_at IS NULL`, in.VisitorVisitID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE visitor_visits SET status='CHECKED_IN',checked_in_at=COALESCE(checked_in_at,now()),badge_no=NULLIF($2,'') WHERE id=$1`, in.VisitorVisitID, in.BadgeNo); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE visits SET status='CHECKED_IN',updated_at=now() WHERE id=$1 AND status IN ('SCHEDULED','APPROVED','ARRIVED')`, visitID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	details, _ := json.Marshal(map[string]string{"reason": in.Reason})
	if _, err = tx.Exec(r.Context(), `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,actor_user_id,lobby_id,method,details) VALUES($1,$2,'CHECKED_IN',NULLIF($3,''),NULLIF($4,''),'manual',$5)`, visitID, in.VisitorVisitID, u.ID, in.LobbyID, details); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = s.queueNotificationEventTx(r.Context(), tx, visitID, in.VisitorVisitID, "checked_in", now); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.metrics.checkIns.Add(1)
	s.audit(r.Context(), u.ID, "visit.checkin", "visitor_visit", in.VisitorVisitID, clientIP(r), map[string]string{"visitId": visitID, "method": "manual", "reason": in.Reason, "lobbyId": in.LobbyID})
	s.publishLobbyEvent("visitor.checked_in")
	writeJSON(w, http.StatusCreated, map[string]any{"visitorVisitId": in.VisitorVisitID, "visitId": visitID, "checkedInAt": now})
}

// visitDetailExtras returns what the summary row omits but the host and the
// approver need: the decision, the host's memo and the event timeline.
func (s *Server) visitDetailExtras(ctx context.Context, visitID string) (map[string]any, error) {
	var notes, approvalReason, approverName string
	var approvedAt *time.Time
	var recurrence []byte
	err := s.db.QueryRow(ctx, `SELECT COALESCE(v.notes,''),COALESCE(v.approval_reason,''),COALESCE(a.display_name,''),v.approved_at,v.recurrence
		FROM visits v LEFT JOIN users a ON a.id=v.approved_by WHERE v.id=$1`, visitID).Scan(&notes, &approvalReason, &approverName, &approvedAt, &recurrence)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT e.event_type,COALESCE(u.display_name,''),COALESCE(l.name,''),COALESCE(e.method,''),e.details,e.created_at,
		COALESCE(p.name_encrypted,''),p.masked_at
		FROM visit_events e LEFT JOIN users u ON u.id=e.actor_user_id LEFT JOIN lobbies l ON l.id=e.lobby_id
		LEFT JOIN visitor_visits vv ON vv.id=e.visitor_visit_id LEFT JOIN visitors p ON p.id=vv.visitor_id
		WHERE e.visit_id=$1 ORDER BY e.created_at,e.id`, visitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []map[string]any{}
	for rows.Next() {
		var eventType, actor, lobby, method, nameEnc string
		var details []byte
		var createdAt time.Time
		var maskedAt *time.Time
		if rows.Scan(&eventType, &actor, &lobby, &method, &details, &createdAt, &nameEnc, &maskedAt) != nil {
			continue
		}
		var detailValue any
		_ = json.Unmarshal(details, &detailValue)
		visitor := s.decryptOptional(nameEnc)
		if maskedAt != nil {
			visitor = maskName(visitor)
		}
		events = append(events, map[string]any{"type": eventType, "actor": actor, "lobby": lobby, "method": method, "details": detailValue, "visitor": visitor, "createdAt": createdAt})
	}
	var recurrenceValue map[string]any
	_ = json.Unmarshal(recurrence, &recurrenceValue)
	return map[string]any{
		"notes": notes, "approvalReason": approvalReason, "approverName": approverName, "approvedAt": approvedAt,
		"seriesId": recurrenceValue["seriesId"], "events": events,
	}, rows.Err()
}
