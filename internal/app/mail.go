package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// smtpConfig reads the relay settings. enabled is false when the operator has
// not switched mail on, which every caller must treat as "do nothing quietly".
func (s *Server) smtpConfig(ctx context.Context) (platform.SMTPConfig, bool) {
	port, _ := strconv.Atoi(settingOr(s, ctx, "smtp.port", "587"))
	cfg := platform.SMTPConfig{
		Host:          settingOr(s, ctx, "smtp.host", ""),
		Port:          port,
		Username:      settingOr(s, ctx, "smtp.username", ""),
		Password:      settingOr(s, ctx, "smtp.password", ""),
		From:          settingOr(s, ctx, "smtp.from", ""),
		Security:      settingOr(s, ctx, "smtp.security", "starttls"),
		SkipTLSVerify: settingOr(s, ctx, "smtp.skip_tls_verify", "false") == "true",
		Timeout:       15 * time.Second,
	}
	return cfg, settingOr(s, ctx, "smtp.enabled", "false") == "true"
}

func maskEmail(value string) string {
	at := strings.Index(value, "@")
	if at <= 1 {
		return "***"
	}
	return value[:1] + strings.Repeat("*", at-1) + value[at:]
}

// testSMTP sends one message with the saved relay settings so the operator can
// confirm host, TLS mode and credentials before anything depends on them.
func (s *Server) testSMTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(in.To)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_recipient", "수신 이메일 주소를 확인하세요")
		return
	}
	cfg, enabled := s.smtpConfig(r.Context())
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "smtp_incomplete", err.Error())
		return
	}
	company := settingOr(s, r.Context(), "general.company_name", "VisitFlow")
	started := time.Now()
	err := platform.SendMail(r.Context(), cfg, platform.Mail{
		To:      []string{strings.TrimSpace(in.To)},
		Subject: "[VisitFlow] SMTP 연결 테스트",
		Body:    fmt.Sprintf("%s VisitFlow에서 보낸 SMTP 테스트 메일입니다.\n\n서버 %s:%d · 보안 %s · 발신 %s\n시각 %s", company, cfg.Host, cfg.Port, cfg.Security, cfg.From, time.Now().Format("2006-01-02 15:04:05")),
	})
	u, _ := userFrom(r)
	result := map[string]any{"ok": err == nil, "durationMs": time.Since(started).Milliseconds(), "enabled": enabled, "to": maskEmail(strings.TrimSpace(in.To))}
	if err != nil {
		result["error"] = err.Error()
	}
	s.audit(r.Context(), u.ID, "smtp.test", "settings", "", r.RemoteAddr, result)
	writeJSON(w, http.StatusOK, result)
}

// ---- password reset by e-mail ----

func (s *Server) passwordResetAvailable(ctx context.Context) bool {
	_, smtpEnabled := s.smtpConfig(ctx)
	return smtpEnabled && settingOr(s, ctx, "auth.local_enabled", "true") == "true" && settingOr(s, ctx, "auth.password_reset_enabled", "true") == "true"
}

// issuePasswordReset creates a single-use token and mails the link. It returns
// nil when the user cannot receive mail, so callers can stay silent about
// whether an account exists.
func (s *Server) issuePasswordReset(ctx context.Context, r *http.Request, userID, requestedBy string) (bool, error) {
	var email, displayName, source string
	var active bool
	err := s.db.QueryRow(ctx, `SELECT COALESCE(email,''),display_name,source,active FROM users WHERE id=$1`, userID).Scan(&email, &displayName, &source, &active)
	if err != nil {
		return false, err
	}
	if !active || source != "local" || email == "" {
		return false, nil
	}
	minutes, _ := strconv.Atoi(settingOr(s, ctx, "auth.password_reset_minutes", "30"))
	if minutes < 5 || minutes > 1440 {
		minutes = 30
	}
	random, err := platform.RandomToken(32)
	if err != nil {
		return false, err
	}
	token := "vfp_" + random
	expires := time.Now().Add(time.Duration(minutes) * time.Minute)
	if _, err = s.db.Exec(ctx, `INSERT INTO password_resets(token_hash,user_id,requested_by,ip_address,expires_at) VALUES($1,$2,NULLIF($3,''),$4,$5)`,
		s.keys.Digest("reset:"+token), userID, requestedBy, requestRemote(r), expires); err != nil {
		return false, err
	}
	cfg, _ := s.smtpConfig(ctx)
	link := strings.TrimRight(s.publicBaseURL(ctx, r), "/") + "/reset-password/" + token
	company := settingOr(s, ctx, "general.company_name", "VisitFlow")
	body := fmt.Sprintf("%s 님,\n\n%s VisitFlow 비밀번호 재설정 요청이 접수되었습니다. 아래 링크에서 새 비밀번호를 설정하세요.\n\n%s\n\n이 링크는 %d분 동안 한 번만 사용할 수 있습니다. 본인이 요청하지 않았다면 이 메일을 무시하세요. 기존 비밀번호는 바뀌지 않습니다.\n",
		displayName, company, link, minutes)
	if err := platform.SendMail(ctx, cfg, platform.Mail{To: []string{email}, Subject: "[VisitFlow] 비밀번호 재설정 안내", Body: body}); err != nil {
		s.logger.Error("password reset mail failed", "error", err, "user", userID)
		return false, err
	}
	return true, nil
}

// requestPasswordReset is the public "forgot password" entry point. It always
// answers 202 so it cannot be used to enumerate accounts, and it is throttled
// per address and per identifier.
func (s *Server) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Identifier string `json:"identifier"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	identifier := strings.ToLower(strings.TrimSpace(in.Identifier))
	if identifier == "" {
		writeError(w, http.StatusBadRequest, "identifier_required", "아이디 또는 이메일을 입력하세요")
		return
	}
	if !s.passwordResetAvailable(r.Context()) {
		writeError(w, http.StatusNotFound, "password_reset_disabled", "메일 비밀번호 재설정이 활성화되어 있지 않습니다. 관리자에게 문의하세요")
		return
	}
	if allowed, _ := s.publicLimiter.allow("reset-id|"+identifier, 3, time.Now()); !allowed {
		s.metrics.rateLimited.Add(1)
		writeError(w, http.StatusTooManyRequests, "rate_limited", "잠시 후 다시 시도하세요")
		return
	}
	var userID string
	err := s.db.QueryRow(r.Context(), `SELECT id FROM users WHERE active AND source='local' AND (lower(username)=$1 OR lower(email)=$1) ORDER BY username LIMIT 1`, identifier).Scan(&userID)
	sent := false
	if err == nil {
		sent, err = s.issuePasswordReset(r.Context(), r, userID, "")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.logger.Error("password reset request failed", "error", err)
	}
	s.audit(r.Context(), "", "auth.password_reset_request", "user", userID, r.RemoteAddr, map[string]any{"identifier": identifier, "sent": sent})
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "message": "계정이 확인되면 등록된 이메일로 재설정 링크를 보냈습니다. 몇 분 안에 도착하지 않으면 스팸함을 확인하거나 관리자에게 문의하세요."})
}

func (s *Server) lookupPasswordReset(ctx context.Context, token string) (userID, username string, err error) {
	err = s.db.QueryRow(ctx, `SELECT u.id,u.username FROM password_resets p JOIN users u ON u.id=p.user_id
		WHERE p.token_hash=$1 AND p.used_at IS NULL AND p.expires_at>now() AND u.active AND u.source='local'`, s.keys.Digest("reset:"+token)).Scan(&userID, &username)
	return
}

func (s *Server) checkPasswordReset(w http.ResponseWriter, r *http.Request) {
	_, username, err := s.lookupPasswordReset(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeError(w, http.StatusNotFound, "reset_invalid", "재설정 링크가 만료되었거나 이미 사용되었습니다")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "username": maskName(username)})
}

func (s *Server) completePasswordReset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NewPassword string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.NewPassword) < 12 {
		writeError(w, http.StatusBadRequest, "weak_password", "새 비밀번호는 12자 이상이어야 합니다")
		return
	}
	token := chi.URLParam(r, "token")
	userID, username, err := s.lookupPasswordReset(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "reset_invalid", "재설정 링크가 만료되었거나 이미 사용되었습니다")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), 12)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Marking the token used first makes a concurrent second submit lose.
	tag, err := tx.Exec(r.Context(), `UPDATE password_resets SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL`, s.keys.Digest("reset:"+token))
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "reset_used", "이 링크는 이미 사용되었습니다")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,must_change_password=false,updated_at=now() WHERE id=$1`, userID, string(hash)); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM auth_throttle WHERE key=$1`, "user:"+strings.ToLower(username)); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE password_resets SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, userID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), userID, "auth.password_reset_complete", "user", userID, r.RemoteAddr, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- per-user mail alert preferences ----

// mailEvents lists what a person can subscribe to. Approval events only reach
// users who can approve; the rest concern the host of a visit.
var mailEvents = []string{"checked_in", "checked_out", "visit_confirmed", "visit_rejected", "visit_cancelled", "approval_pending", "approval_escalated"}

var mailEventDefaults = map[string]bool{"checked_in": true, "checked_out": false, "visit_confirmed": true, "visit_rejected": true, "visit_cancelled": true, "approval_pending": true, "approval_escalated": true}

type mailPreferences struct {
	Enabled bool            `json:"emailEnabled"`
	Events  map[string]bool `json:"events"`
}

func defaultMailPreferences() mailPreferences {
	events := map[string]bool{}
	for key, value := range mailEventDefaults {
		events[key] = value
	}
	return mailPreferences{Enabled: true, Events: events}
}

func parseMailPreferences(raw []byte) mailPreferences {
	prefs := defaultMailPreferences()
	var stored struct {
		Enabled *bool           `json:"emailEnabled"`
		Events  map[string]bool `json:"events"`
	}
	if json.Unmarshal(raw, &stored) != nil {
		return prefs
	}
	if stored.Enabled != nil {
		prefs.Enabled = *stored.Enabled
	}
	for key, value := range stored.Events {
		if _, known := mailEventDefaults[key]; known {
			prefs.Events[key] = value
		}
	}
	return prefs
}

func (s *Server) getMailPreferences(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	var raw []byte
	var email string
	if err := s.db.QueryRow(r.Context(), `SELECT mail_preferences,COALESCE(email,'') FROM users WHERE id=$1`, u.ID).Scan(&raw, &email); err != nil {
		notFoundOrServer(w, err)
		return
	}
	_, smtpEnabled := s.smtpConfig(r.Context())
	prefs := parseMailPreferences(raw)
	writeJSON(w, http.StatusOK, map[string]any{"emailEnabled": prefs.Enabled, "events": prefs.Events, "availableEvents": mailEvents, "email": maskEmail(email), "hasEmail": email != "", "smtpEnabled": smtpEnabled, "canApprove": u.CanApprove()})
}

func (s *Server) updateMailPreferences(w http.ResponseWriter, r *http.Request) {
	var in mailPreferences
	if !decodeJSON(w, r, &in) {
		return
	}
	prefs := defaultMailPreferences()
	prefs.Enabled = in.Enabled
	for key, value := range in.Events {
		if _, known := mailEventDefaults[key]; !known {
			writeError(w, http.StatusBadRequest, "unknown_event", "지원하지 않는 알림 이벤트입니다: "+key)
			return
		}
		prefs.Events[key] = value
	}
	raw, _ := json.Marshal(prefs)
	u, _ := userFrom(r)
	if _, err := s.db.Exec(r.Context(), `UPDATE users SET mail_preferences=$2,updated_at=now() WHERE id=$1`, u.ID, raw); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "profile.mail_preferences", "user", u.ID, r.RemoteAddr, prefs)
	writeJSON(w, http.StatusOK, prefs)
}

// mailRecipient resolves whether one user wants an e-mail for an event and
// where to send it.
func (s *Server) mailRecipientTx(ctx context.Context, tx pgx.Tx, userID, event string) (string, bool) {
	var email string
	var raw []byte
	var active bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(email,''),mail_preferences,active FROM users WHERE id=$1`, userID).Scan(&email, &raw, &active); err != nil || !active || email == "" {
		return "", false
	}
	prefs := parseMailPreferences(raw)
	if !prefs.Enabled || !prefs.Events[event] {
		return "", false
	}
	return email, true
}

var mailSubjects = map[string]string{
	"checked_in":         "[VisitFlow] {{visitor}} 님이 도착했습니다",
	"checked_out":        "[VisitFlow] {{visitor}} 님이 퇴실했습니다",
	"visit_confirmed":    "[VisitFlow] 방문 {{requestNo}} 이(가) 확정되었습니다",
	"visit_rejected":     "[VisitFlow] 방문 {{requestNo}} 이(가) 반려되었습니다",
	"visit_cancelled":    "[VisitFlow] 방문 {{requestNo}} 이(가) 취소되었습니다",
	"approval_pending":   "[VisitFlow] 승인 대기 방문 {{requestNo}}",
	"approval_escalated": "[VisitFlow] 승인 지연 방문 {{requestNo}}",
}

var mailBodies = map[string]string{
	"checked_in":         "{{visitor}} ({{visitorCompany}}) 님이 {{checkedIn}}에 {{lobby}}에 도착해 입실했습니다.\n방문 {{requestNo}} · {{place}} · {{start}}~{{end}}",
	"checked_out":        "{{visitor}} ({{visitorCompany}}) 님이 {{checkedOut}}에 퇴실했습니다.\n방문 {{requestNo}} · {{place}}",
	"visit_confirmed":    "{{visitor}} ({{visitorCompany}}) 님의 방문이 확정되었습니다.\n일시 {{start}}~{{end}} · 장소 {{place}} · 방문번호 {{requestNo}}\n방문자에게 모바일 방문증이 발송됩니다.",
	"visit_rejected":     "{{visitor}} ({{visitorCompany}}) 님의 방문 {{requestNo}} 이(가) 반려되었습니다.\n일시 {{start}}~{{end}} · 장소 {{place}}\n반려 사유는 내 방문 일정에서 확인하세요.",
	"visit_cancelled":    "{{visitor}} ({{visitorCompany}}) 님의 방문 {{requestNo}} 이(가) 취소되었습니다.\n일시 {{start}}~{{end}} · 장소 {{place}}",
	"approval_pending":   "{{host}} 담당 방문 {{requestNo}} 이(가) 승인을 기다립니다.\n방문자 {{visitor}} ({{visitorCompany}}) · 일시 {{start}}~{{end}} · 장소 {{place}}\n방문 승인 화면에서 검토하세요.",
	"approval_escalated": "방문 {{requestNo}} 이(가) 설정된 시간 안에 승인되지 않았습니다.\n방문자 {{visitor}} ({{visitorCompany}}) · 담당 {{host}} · 일시 {{start}}~{{end}}\n방문 승인 화면에서 처리하세요.",
}

// queueMailTx enqueues one e-mail into the notification queue with channel
// "email"; the delivery worker sends it through the configured relay.
func (s *Server) queueMailTx(ctx context.Context, tx pgx.Tx, visitID, visitorVisitID, event, recipient string, variables map[string]string) error {
	subject := renderTemplate(mailSubjects[event], variables)
	body := renderTemplate(mailBodies[event], variables)
	company := settingOr(s, ctx, "general.company_name", "")
	if company != "" {
		body += "\n\n— " + company + " VisitFlow"
	}
	metadata := map[string]string{"subject": subject, "recipient": recipient, "channel": "email", "event": event}
	metadataJSON, _ := json.Marshal(metadata)
	metadataEncrypted, err := s.keys.Encrypt(string(metadataJSON))
	if err != nil {
		return err
	}
	recipientEncrypted, err := s.keys.Encrypt(recipient)
	if err != nil {
		return err
	}
	bodyEncrypted, err := s.keys.Encrypt(body)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(id,visit_id,visitor_visit_id,recipient_encrypted,channel,template_key,body_encrypted,metadata_encrypted,next_attempt_at)
		VALUES($1,$2,$3,$4,'email',$5,$6,$7,now())`, newID(), visitID, visitorVisitID, recipientEncrypted, "mail_"+event, bodyEncrypted, metadataEncrypted)
	return err
}

// queueHostMailTx mails the host (or the active delegate covering for them)
// according to their own preferences. Nothing is queued while SMTP is off.
func (s *Server) queueHostMailTx(ctx context.Context, tx pgx.Tx, data notificationEventData, event string, eventAt time.Time) error {
	if _, enabled := s.smtpConfig(ctx); !enabled {
		return nil
	}
	if _, known := mailSubjects[event]; !known || event == "approval_pending" {
		return nil
	}
	var hostID string
	var delegateID *string
	if err := tx.QueryRow(ctx, `SELECT h.id,CASE WHEN h.delegate_until>now() THEN h.delegate_user_id END FROM visits v JOIN users h ON h.id=v.host_user_id WHERE v.id=$1`, data.VisitID).Scan(&hostID, &delegateID); err != nil {
		return err
	}
	variables := data.variables(eventAt)
	recipients := []string{hostID}
	if delegateID != nil && *delegateID != "" {
		recipients = append(recipients, *delegateID)
	}
	seen := map[string]bool{}
	for _, userID := range recipients {
		email, wanted := s.mailRecipientTx(ctx, tx, userID, event)
		if !wanted || seen[email] {
			continue
		}
		seen[email] = true
		if err := s.queueMailTx(ctx, tx, data.VisitID, data.VisitorVisitID, event, email, variables); err != nil {
			return err
		}
	}
	return nil
}

// queueApproverMailTx tells the department's managers (and their active
// delegates) that a visit is waiting; security and administrators are not
// spammed by default because they see the queue on their dashboard.
func (s *Server) queueApproverMailTx(ctx context.Context, tx pgx.Tx, visitID, visitorVisitID string, eventAt time.Time) error {
	if _, enabled := s.smtpConfig(ctx); !enabled {
		return nil
	}
	data, err := s.notificationEventDataTx(ctx, tx, visitID, visitorVisitID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT m.id,CASE WHEN m.delegate_until>now() THEN m.delegate_user_id END FROM visits v JOIN users m ON m.department_id=v.department_id
		WHERE v.id=$1 AND m.active AND m.role='dept_manager'`, visitID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var managerID string
		var delegateID *string
		if rows.Scan(&managerID, &delegateID) == nil {
			ids = append(ids, managerID)
			if delegateID != nil && *delegateID != "" {
				ids = append(ids, *delegateID)
			}
		}
	}
	rows.Close()
	variables := data.variables(eventAt)
	seen := map[string]bool{}
	for _, userID := range ids {
		email, wanted := s.mailRecipientTx(ctx, tx, userID, "approval_pending")
		if !wanted || seen[email] {
			continue
		}
		seen[email] = true
		if err := s.queueMailTx(ctx, tx, visitID, visitorVisitID, "approval_pending", email, variables); err != nil {
			return err
		}
	}
	return nil
}

// sendQueuedMail is the worker-side delivery for channel "email".
func (s *Server) sendQueuedMail(ctx context.Context, recipient, body string, metadata map[string]string) error {
	cfg, enabled := s.smtpConfig(ctx)
	if !enabled {
		return errors.New("SMTP가 비활성화되어 있습니다")
	}
	subject := metadata["subject"]
	if subject == "" {
		subject = "[VisitFlow] 알림"
	}
	return platform.SendMail(ctx, cfg, platform.Mail{To: []string{recipient}, Subject: subject, Body: body})
}
