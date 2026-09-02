package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5"
)

// consentSource maps the channel a visit arrived through onto the consent
// vocabulary, so every consent row says who asserted it.
func consentSource(visitSource string) string {
	switch visitSource {
	case "lobby", "import", "api", "mcp", "self":
		return visitSource
	default:
		return "host"
	}
}

type consentContext struct {
	Source    string
	Locale    string
	ActorID   string
	IPAddress string
	UserAgent string
}

func consentContextFrom(r *http.Request, actorID, source string) consentContext {
	ctx := consentContext{Source: source, ActorID: actorID}
	if r != nil {
		ctx.IPAddress = clientIP(r)
		ctx.UserAgent = r.UserAgent()
	}
	return ctx
}

// recordConsentTx never fails on an unattended actor: a kiosk or a visitor's own
// browser has no user row, and NULLIF keeps the foreign key satisfied.
func (s *Server) recordConsentTx(ctx context.Context, tx pgx.Tx, visitorID, visitID, visitorVisitID string, meta consentContext) error {
	policyVersion := settingOr(s, ctx, "privacy.consent_policy_version", "1.0")
	_, err := tx.Exec(ctx, `INSERT INTO consent_records(id,visitor_id,visit_id,visitor_visit_id,source,policy_version,locale,actor_user_id,ip_address,user_agent)
		VALUES($1,NULLIF($2,''),NULLIF($3,''),NULLIF($4,''),$5,$6,$7,NULLIF($8,''),$9,$10)`,
		newID(), visitorID, visitID, visitorVisitID, meta.Source, policyVersion, meta.Locale, meta.ActorID, meta.IPAddress, meta.UserAgent)
	return err
}

// createRegistrationInvitation issues a single-use link the visitor uses to
// enter and confirm their own details. It moves the personal data and the
// consent from the host's word to the visitor's own action.
func (s *Server) createRegistrationInvitation(w http.ResponseWriter, r *http.Request) {
	if enabled := settingOr(s, r.Context(), "visit.self_registration_enabled", "true"); enabled != "true" {
		writeError(w, http.StatusForbidden, "self_registration_disabled", "방문자 사전등록이 비활성화되어 있습니다")
		return
	}
	participantID := chi.URLParam(r, "visitorVisitID")
	u, _ := userFrom(r)
	var visitID, hostID, visitStatus, participantStatus string
	err := s.db.QueryRow(r.Context(), `SELECT v.id,v.host_user_id,v.status,vv.status FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE vv.id=$1`, participantID).
		Scan(&visitID, &hostID, &visitStatus, &participantStatus)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !s.actsForHost(r.Context(), u, hostID) && !u.CanManageLobby() && !u.IsAdmin() {
		writeError(w, http.StatusForbidden, "forbidden", "사전등록 링크를 만들 권한이 없습니다")
		return
	}
	if qrParticipantStatusTerminal(participantStatus) || qrParticipantStatusTerminal(visitStatus) {
		writeError(w, http.StatusConflict, "visit_not_open", "종료·취소된 방문은 사전등록할 수 없습니다")
		return
	}
	hours, _ := strconv.Atoi(settingOr(s, r.Context(), "visit.self_registration_hours", "72"))
	if hours < 1 || hours > 720 {
		hours = 72
	}
	random, err := platform.RandomToken(32)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	token := "vfr_" + random
	expiresAt := time.Now().Add(time.Duration(hours) * time.Hour)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// One open invitation per participant: re-issuing replaces the previous link.
	if _, err = tx.Exec(r.Context(), `UPDATE registration_invitations SET revoked_at=now() WHERE visitor_visit_id=$1 AND completed_at IS NULL AND revoked_at IS NULL`, participantID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	id := newID()
	if _, err = tx.Exec(r.Context(), `INSERT INTO registration_invitations(id,visitor_visit_id,token_hash,created_by,expires_at) VALUES($1,$2,$3,NULLIF($4,''),$5)`,
		id, participantID, s.keys.Digest("registration:"+token), u.ID, expiresAt); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "registration.invite", "visitor_visit", participantID, clientIP(r), map[string]any{"visitId": visitID, "expiresAt": expiresAt})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "expiresAt": expiresAt,
		"registrationUrl": strings.TrimRight(s.publicBaseURL(r.Context(), r), "/") + "/r/" + token,
	})
}

func (s *Server) revokeRegistrationInvitation(w http.ResponseWriter, r *http.Request) {
	participantID := chi.URLParam(r, "visitorVisitID")
	u, _ := userFrom(r)
	var hostID string
	if err := s.db.QueryRow(r.Context(), `SELECT v.host_user_id FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE vv.id=$1`, participantID).Scan(&hostID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !s.actsForHost(r.Context(), u, hostID) && !u.CanManageLobby() && !u.IsAdmin() {
		writeError(w, http.StatusForbidden, "forbidden", "사전등록 링크를 폐기할 권한이 없습니다")
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE registration_invitations SET revoked_at=now() WHERE visitor_visit_id=$1 AND completed_at IS NULL AND revoked_at IS NULL`, participantID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		notFoundOrServer(w, pgx.ErrNoRows)
		return
	}
	s.audit(r.Context(), u.ID, "registration.revoke", "visitor_visit", participantID, clientIP(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

type registrationTarget struct {
	InvitationID, ParticipantID, VisitID, VisitorID string
	VisitStatus, ParticipantStatus                  string
	Name, Company, Title, Email, Vehicle, Locale    string
	Equipment                                       []string
	Host, Department, Site, Lobby, Purpose          string
	StartAt, EndAt                                  time.Time
	VisitType                                       VisitType
	HasVisitType                                    bool
}

func (s *Server) loadRegistrationTarget(ctx context.Context, token string) (registrationTarget, error) {
	var target registrationTarget
	var nameEnc, phoneEnc, emailEnc, vehicleEnc string
	var equipment []byte
	var visitTypeID *string
	err := s.db.QueryRow(ctx, `SELECT ri.id,vv.id,v.id,p.id,v.status,vv.status,p.name_encrypted,p.phone_encrypted,COALESCE(p.email_encrypted,''),COALESCE(p.company,''),COALESCE(p.title,''),COALESCE(p.vehicle_encrypted,''),p.locale,vv.equipment,
		h.display_name,COALESCE(o.name,''),si.name,COALESCE(l.name,''),v.purpose,v.start_at,v.end_at,v.visit_type_id
		FROM registration_invitations ri
		JOIN visitor_visits vv ON vv.id=ri.visitor_visit_id
		JOIN visits v ON v.id=vv.visit_id
		JOIN visitors p ON p.id=vv.visitor_id
		JOIN users h ON h.id=v.host_user_id
		JOIN sites si ON si.id=v.site_id
		LEFT JOIN lobbies l ON l.id=v.lobby_id
		LEFT JOIN organizations o ON o.id=v.department_id
		WHERE ri.token_hash=$1 AND ri.revoked_at IS NULL AND ri.completed_at IS NULL AND ri.expires_at>now()`,
		s.keys.Digest("registration:"+token)).
		Scan(&target.InvitationID, &target.ParticipantID, &target.VisitID, &target.VisitorID, &target.VisitStatus, &target.ParticipantStatus,
			&nameEnc, &phoneEnc, &emailEnc, &target.Company, &target.Title, &vehicleEnc, &target.Locale, &equipment,
			&target.Host, &target.Department, &target.Site, &target.Lobby, &target.Purpose, &target.StartAt, &target.EndAt, &visitTypeID)
	if err != nil {
		return target, err
	}
	target.Name = s.decryptOptional(nameEnc)
	target.Email = s.decryptOptional(emailEnc)
	target.Vehicle = s.decryptOptional(vehicleEnc)
	_ = json.Unmarshal(equipment, &target.Equipment)
	if visitTypeID != nil && *visitTypeID != "" {
		if visitType, typeErr := scanVisitType(s.db.QueryRow(ctx, visitTypeSelect+` WHERE id=$1`, *visitTypeID)); typeErr == nil {
			target.VisitType, target.HasVisitType = visitType, true
		}
	}
	return target, nil
}

func (s *Server) publicRegistration(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	target, err := s.loadRegistrationTarget(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "registration_not_found", "사전등록 링크가 만료되었거나 이미 완료되었습니다")
		return
	}
	if qrParticipantStatusTerminal(target.VisitStatus) || qrParticipantStatusTerminal(target.ParticipantStatus) {
		writeError(w, http.StatusConflict, "visit_not_open", "종료되었거나 취소된 방문입니다")
		return
	}
	locale := s.negotiateLocale(r.Context(), r.URL.Query().Get("lang"), target.Locale, r.Header.Get("Accept-Language"))
	writeJSON(w, http.StatusOK, map[string]any{
		"locale": locale, "supportedLocales": s.supportedLocales(r.Context()),
		"policyVersion": settingOr(s, r.Context(), "privacy.consent_policy_version", "1.0"),
		"companyName":   settingOr(s, r.Context(), "general.company_name", ""),
		"visit": map[string]any{
			"host": maskName(target.Host), "department": target.Department, "site": target.Site, "lobby": target.Lobby,
			"purpose": target.Purpose, "startAt": target.StartAt, "endAt": target.EndAt,
		},
		"visitor": map[string]any{
			"name": target.Name, "company": target.Company, "title": target.Title,
			"email": target.Email, "vehicle": target.Vehicle, "equipment": target.Equipment,
		},
		"requires": map[string]bool{
			"nda":            target.HasVisitType && target.VisitType.RequiresNDA,
			"safetyBriefing": target.HasVisitType && target.VisitType.RequiresSafetyBriefing,
			"vehicle":        target.HasVisitType && target.VisitType.RequiresVehicle,
			"equipment":      target.HasVisitType && target.VisitType.RequiresEquipment,
		},
		"visitType": map[string]any{"name": target.VisitType.Name, "description": target.VisitType.Description},
	})
}

func (s *Server) submitPublicRegistration(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string   `json:"name"`
		Company        string   `json:"company"`
		Title          string   `json:"title"`
		Email          string   `json:"email"`
		Vehicle        string   `json:"vehicle"`
		Equipment      []string `json:"equipment"`
		Locale         string   `json:"locale"`
		Consent        bool     `json:"consent"`
		NDA            bool     `json:"nda"`
		SafetyBriefing bool     `json:"safetyBriefing"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	token := chi.URLParam(r, "token")
	target, err := s.loadRegistrationTarget(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "registration_not_found", "사전등록 링크가 만료되었거나 이미 완료되었습니다")
		return
	}
	if qrParticipantStatusTerminal(target.VisitStatus) || qrParticipantStatusTerminal(target.ParticipantStatus) {
		writeError(w, http.StatusConflict, "visit_not_open", "종료되었거나 취소된 방문입니다")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Company = strings.TrimSpace(in.Company)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name_required", "이름을 입력하세요")
		return
	}
	if !in.Consent {
		writeError(w, http.StatusBadRequest, "consent_required", "개인정보 수집·이용 동의가 필요합니다")
		return
	}
	if target.HasVisitType {
		if target.VisitType.RequiresNDA && !in.NDA {
			writeError(w, http.StatusBadRequest, "nda_required", "보안서약 확인이 필요합니다")
			return
		}
		if target.VisitType.RequiresSafetyBriefing && !in.SafetyBriefing {
			writeError(w, http.StatusBadRequest, "safety_required", "안전교육 이수 확인이 필요합니다")
			return
		}
		if target.VisitType.RequiresVehicle && strings.TrimSpace(in.Vehicle) == "" {
			writeError(w, http.StatusBadRequest, "vehicle_required", "차량번호를 입력하세요")
			return
		}
		if target.VisitType.RequiresEquipment && len(in.Equipment) == 0 {
			writeError(w, http.StatusBadRequest, "equipment_required", "반입 장비를 입력하세요")
			return
		}
	}
	// A company entered by the visitor has never passed the watch list check the
	// host's submission went through, so re-run it here.
	if !strings.EqualFold(strings.TrimSpace(in.Company), strings.TrimSpace(target.Company)) && in.Company != "" {
		var watchID string
		watchErr := s.db.QueryRow(r.Context(), `SELECT id FROM watchlist_entries WHERE active AND starts_at<=now() AND (ends_at IS NULL OR ends_at>now()) AND company<>'' AND lower(company)=lower($1) LIMIT 1`, in.Company).Scan(&watchID)
		if watchErr == nil {
			s.audit(r.Context(), "", "watchlist.match", "watchlist", watchID, clientIP(r), map[string]string{"source": "self"})
			writeError(w, http.StatusForbidden, "visit_restricted", "보안 정책에 따라 사전등록을 완료할 수 없습니다. 담당자에게 문의하세요")
			return
		}
		if watchErr != nil && !errors.Is(watchErr, pgx.ErrNoRows) {
			notFoundOrServer(w, watchErr)
			return
		}
	}
	locale := normalizeLocale(in.Locale)
	nameEnc, err := s.keys.Encrypt(in.Name)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	emailEnc, err := s.encryptOptional(in.Email)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	vehicleEnc, err := s.encryptOptional(in.Vehicle)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	equipment := []string{}
	for _, item := range in.Equipment {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			equipment = append(equipment, trimmed)
		}
	}
	equipmentJSON, err := json.Marshal(equipment)
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
	if _, err = tx.Exec(r.Context(), `UPDATE visitors SET name_encrypted=$2,name_hash=$3,email_encrypted=NULLIF($4,''),company=NULLIF($5,''),title=NULLIF($6,''),vehicle_encrypted=NULLIF($7,''),locale=$8,consented_at=now(),masked_at=NULL,updated_at=now() WHERE id=$1`,
		target.VisitorID, nameEnc, s.keys.Digest("name:"+strings.ToLower(in.Name)), emailEnc, in.Company, strings.TrimSpace(in.Title), vehicleEnc, locale); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE visitor_visits SET equipment=$2 WHERE id=$1`, target.ParticipantID, equipmentJSON); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = s.recordConsentTx(r.Context(), tx, target.VisitorID, target.VisitID, target.ParticipantID,
		consentContext{Source: "self", Locale: locale, IPAddress: clientIP(r), UserAgent: r.UserAgent()}); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE registration_invitations SET completed_at=now() WHERE id=$1`, target.InvitationID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	details, _ := json.Marshal(map[string]any{"source": "self", "nda": in.NDA, "safetyBriefing": in.SafetyBriefing})
	if _, err = tx.Exec(r.Context(), `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,method,details) VALUES($1,$2,'REGISTERED','self',$3)`,
		target.VisitID, target.ParticipantID, details); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), "", "registration.complete", "visitor_visit", target.ParticipantID, clientIP(r), map[string]any{"visitId": target.VisitID, "locale": locale})
	s.publishLobbyEvent("visit.updated")
	writeJSON(w, http.StatusOK, map[string]any{"completed": true, "locale": locale})
}
