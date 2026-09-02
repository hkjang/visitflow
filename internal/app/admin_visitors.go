package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// visitorDetail is the security officer's view of one person: identity on
// file, consent history and every visit they took part in.
func (s *Server) visitorDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "visitorID")
	var nameEnc, phoneEnc, emailEnc, company, title, vehicleEnc, locale string
	var maskedAt, erasedAt *time.Time
	var createdAt time.Time
	err := s.db.QueryRow(r.Context(), `SELECT name_encrypted,phone_encrypted,COALESCE(email_encrypted,''),COALESCE(company,''),COALESCE(title,''),COALESCE(vehicle_encrypted,''),locale,masked_at,erased_at,created_at FROM visitors WHERE id=$1`, id).
		Scan(&nameEnc, &phoneEnc, &emailEnc, &company, &title, &vehicleEnc, &locale, &maskedAt, &erasedAt, &createdAt)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	name, email, vehicle := s.decryptOptional(nameEnc), s.decryptOptional(emailEnc), s.decryptOptional(vehicleEnc)
	if maskedAt != nil {
		name, email, vehicle = maskName(name), "", ""
	}
	rows, err := s.db.Query(r.Context(), `SELECT v.id,v.request_no,v.start_at,v.end_at,v.status,vv.status,si.name,h.display_name,v.purpose,vv.checked_in_at,vv.checked_out_at
		FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites si ON si.id=v.site_id JOIN users h ON h.id=v.host_user_id
		WHERE vv.visitor_id=$1 ORDER BY v.start_at DESC LIMIT 200`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	visits := []map[string]any{}
	for rows.Next() {
		var visitID, requestNo, visitStatus, participantStatus, site, host, purpose string
		var startAt, endAt time.Time
		var checkedIn, checkedOut *time.Time
		if rows.Scan(&visitID, &requestNo, &startAt, &endAt, &visitStatus, &participantStatus, &site, &host, &purpose, &checkedIn, &checkedOut) == nil {
			visits = append(visits, map[string]any{"id": visitID, "requestNo": requestNo, "startAt": startAt, "endAt": endAt, "status": visitStatus, "participantStatus": participantStatus, "site": site, "host": host, "purpose": purpose, "checkedInAt": checkedIn, "checkedOutAt": checkedOut})
		}
	}
	rows.Close()
	consents := []map[string]any{}
	rows, err = s.db.Query(r.Context(), `SELECT source,policy_version,locale,consented_at FROM consent_records WHERE visitor_id=$1 ORDER BY consented_at DESC LIMIT 50`, id)
	if err == nil {
		for rows.Next() {
			var source, policy, consentLocale string
			var at time.Time
			if rows.Scan(&source, &policy, &consentLocale, &at) == nil {
				consents = append(consents, map[string]any{"source": source, "policyVersion": policy, "locale": consentLocale, "consentedAt": at})
			}
		}
		rows.Close()
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visitor.view", "visitor", id, r.RemoteAddr, map[string]any{"visits": len(visits)})
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "name": name, "phone": maskPhone(s.decryptOptional(phoneEnc)), "email": email, "company": company, "title": title, "vehicle": vehicle,
		"locale": locale, "maskedAt": maskedAt, "erasedAt": erasedAt, "createdAt": createdAt, "visits": visits, "consents": consents,
	})
}

// eraseVisitor fulfils a deletion request immediately instead of waiting for the
// retention job. Statistics keep the anonymised participation rows; identity
// fields become unrecoverable and future visits with the same number no longer
// link to this record.
func (s *Server) eraseVisitor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "visitorID")
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason_required", "파기 사유(요청 근거)를 입력하세요")
		return
	}
	var open int
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM visitor_visits WHERE visitor_id=$1 AND status IN ('PENDING_APPROVAL','SCHEDULED','ARRIVED','CHECKED_IN')`, id).Scan(&open); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if open > 0 {
		writeError(w, http.StatusConflict, "visitor_has_open_visits", "진행 중이거나 예정된 방문이 있는 방문자는 먼저 방문을 취소하거나 종료해야 합니다")
		return
	}
	erased, err := s.keys.Encrypt("파기됨")
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
	var phoneEnc string
	if err = tx.QueryRow(r.Context(), `SELECT phone_encrypted FROM visitors WHERE id=$1 AND erased_at IS NULL FOR UPDATE`, id).Scan(&phoneEnc); err != nil {
		notFoundOrServer(w, err)
		return
	}
	phone := s.decryptOptional(phoneEnc)
	if _, err = tx.Exec(r.Context(), `UPDATE visitors SET name_encrypted=$2,name_hash=NULL,phone_encrypted=$2,phone_hash=NULL,email_encrypted=NULL,vehicle_encrypted=NULL,company=NULL,title=NULL,masked_at=COALESCE(masked_at,now()),erased_at=now(),updated_at=now() WHERE id=$1`, id, erased); err != nil {
		notFoundOrServer(w, err)
		return
	}
	// Address-book copies held by hosts carry the same identity.
	frequent, err := tx.Exec(r.Context(), `DELETE FROM frequent_visitors WHERE phone_hash=$1`, s.keys.Digest("frequent-phone:"+normalizePhone(phone)))
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE registration_invitations SET revoked_at=now() WHERE completed_at IS NULL AND revoked_at IS NULL AND visitor_visit_id IN (SELECT id FROM visitor_visits WHERE visitor_id=$1)`, id); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "privacy.erase_request", "visitor", id, r.RemoteAddr, map[string]any{"reason": in.Reason, "frequentVisitorsRemoved": frequent.RowsAffected()})
	writeJSON(w, http.StatusOK, map[string]any{"erased": true, "frequentVisitorsRemoved": frequent.RowsAffected()})
}
