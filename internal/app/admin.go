package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	queries := map[string]string{
		"today":           `SELECT count(*) FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE v.start_at::date=CURRENT_DATE AND vv.status NOT IN ('CANCELLED','REJECTED')`,
		"current":         `SELECT count(*) FROM visitor_visits WHERE status='CHECKED_IN'`,
		"pendingApproval": `SELECT count(*) FROM visits WHERE status='PENDING_APPROVAL'`,
		"noShow":          `SELECT count(*) FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id WHERE v.start_at::date=CURRENT_DATE AND vv.status='NO_SHOW'`,
		"failedMessages":  `SELECT count(*) FROM notifications WHERE status='failed'`,
		"watchlist":       `SELECT count(*) FROM watchlist_entries WHERE active AND (ends_at IS NULL OR ends_at>now())`,
	}
	for name, query := range queries {
		var count int
		if err := s.db.QueryRow(r.Context(), query).Scan(&count); err != nil {
			notFoundOrServer(w, err)
			return
		}
		counts[name] = count
	}
	writeJSON(w, 200, map[string]any{"counts": counts})
}

func (s *Server) statistics(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 366 {
		days = 30
	}
	rows, err := s.db.Query(r.Context(), `SELECT d::date,COALESCE(count(DISTINCT vv.id),0),COALESCE(count(DISTINCT vv.id) FILTER(WHERE vv.status IN ('CHECKED_IN','CHECKED_OUT')),0) FROM generate_series(CURRENT_DATE-($1::int-1),CURRENT_DATE,interval '1 day') d LEFT JOIN visits v ON v.start_at::date=d::date LEFT JOIN visitor_visits vv ON vv.visit_id=v.id GROUP BY d ORDER BY d`, days)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	daily := []map[string]any{}
	for rows.Next() {
		var day time.Time
		var scheduled, checked int
		if rows.Scan(&day, &scheduled, &checked) == nil {
			daily = append(daily, map[string]any{"date": day.Format("2006-01-02"), "scheduled": scheduled, "checkedIn": checked})
		}
	}
	rows.Close()
	byDepartment := []map[string]any{}
	rows, err = s.db.Query(r.Context(), `SELECT COALESCE(o.name,'미지정'),count(vv.id) FROM visits v JOIN visitor_visits vv ON vv.visit_id=v.id LEFT JOIN organizations o ON o.id=v.department_id WHERE v.start_at>=CURRENT_DATE-$1::int GROUP BY o.name ORDER BY count(vv.id) DESC LIMIT 20`, days)
	if err == nil {
		for rows.Next() {
			var name string
			var count int
			if rows.Scan(&name, &count) == nil {
				byDepartment = append(byDepartment, map[string]any{"name": name, "count": count})
			}
		}
		rows.Close()
	}
	writeJSON(w, 200, map[string]any{"days": days, "daily": daily, "byDepartment": byDepartment})
}

func (s *Server) auditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 200
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	rows, err := s.db.Query(r.Context(), `SELECT a.id,COALESCE(u.display_name,'system'),a.action,a.resource_type,COALESCE(a.resource_id,''),COALESCE(a.ip_address,''),a.details,a.created_at FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id WHERE ($1='' OR a.action ILIKE $1||'%%') ORDER BY a.created_at DESC LIMIT $2`, action, limit)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, act, resourceType, resourceID, ip string
		var details []byte
		var created time.Time
		if rows.Scan(&id, &actor, &act, &resourceType, &resourceID, &ip, &details, &created) == nil {
			var safe any
			_ = json.Unmarshal(details, &safe)
			items = append(items, map[string]any{"id": id, "actor": actor, "action": act, "resourceType": resourceType, "resourceId": resourceID, "ipAddress": ip, "details": safe, "createdAt": created})
		}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT n.id,n.visit_id,n.channel,n.template_key,n.status,n.attempts,COALESCE(n.error,''),n.created_at,n.sent_at,n.recipient_encrypted FROM notifications n ORDER BY n.created_at DESC LIMIT 300`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, channel, key, status, errorText, recipientEnc string
		var visitID *string
		var attempts int
		var created time.Time
		var sent *time.Time
		if rows.Scan(&id, &visitID, &channel, &key, &status, &attempts, &errorText, &created, &sent, &recipientEnc) == nil {
			items = append(items, map[string]any{"id": id, "visitId": visitID, "channel": channel, "templateKey": key, "status": status, "attempts": attempts, "error": errorText, "recipient": maskPhone(s.decryptOptional(recipientEnc)), "createdAt": created, "sentAt": sent})
		}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) listVisitors(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT p.id,p.name_encrypted,p.phone_encrypted,COALESCE(p.company,''),count(vv.id),max(v.start_at),p.erased_at FROM visitors p LEFT JOIN visitor_visits vv ON vv.visitor_id=p.id LEFT JOIN visits v ON v.id=vv.visit_id GROUP BY p.id ORDER BY max(v.start_at) DESC NULLS LAST LIMIT 300`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, nameEnc, phoneEnc, company string
		var visits int
		var lastVisit, erased *time.Time
		if rows.Scan(&id, &nameEnc, &phoneEnc, &company, &visits, &lastVisit, &erased) == nil {
			items = append(items, map[string]any{"id": id, "name": s.decryptOptional(nameEnc), "phone": maskPhone(s.decryptOptional(phoneEnc)), "company": company, "visitCount": visits, "lastVisitAt": lastVisit, "erasedAt": erased})
		}
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visitor.list", "visitor", "", r.RemoteAddr, map[string]int{"count": len(items)})
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) upsertSite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string `json:"id"`
		Code    string `json:"code"`
		Name    string `json:"name"`
		Address string `json:"address"`
		MapURL  string `json:"mapUrl"`
		Active  *bool  `json:"active"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "required_fields", "사업장 코드와 이름은 필수입니다")
		return
	}
	if in.ID == "" {
		in.ID = newID()
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO sites(id,code,name,address,map_url,active) VALUES($1,upper($2),$3,NULLIF($4,''),NULLIF($5,''),$6) ON CONFLICT(id) DO UPDATE SET code=EXCLUDED.code,name=EXCLUDED.name,address=EXCLUDED.address,map_url=EXCLUDED.map_url,active=EXCLUDED.active,updated_at=now()`, in.ID, strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), strings.TrimSpace(in.Address), strings.TrimSpace(in.MapURL), active)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "site.upsert", "site", in.ID, r.RemoteAddr, map[string]string{"code": in.Code, "name": in.Name})
	writeJSON(w, 200, map[string]string{"id": in.ID})
}

func (s *Server) upsertLobby(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID           string `json:"id"`
		SiteID       string `json:"siteId"`
		Code         string `json:"code"`
		Name         string `json:"name"`
		Instructions string `json:"instructions"`
		Active       *bool  `json:"active"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.SiteID == "" || strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "required_fields", "사업장, 로비 코드와 이름은 필수입니다")
		return
	}
	if in.ID == "" {
		in.ID = newID()
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO lobbies(id,site_id,code,name,instructions,active) VALUES($1,$2,upper($3),$4,NULLIF($5,''),$6) ON CONFLICT(id) DO UPDATE SET site_id=EXCLUDED.site_id,code=EXCLUDED.code,name=EXCLUDED.name,instructions=EXCLUDED.instructions,active=EXCLUDED.active,updated_at=now()`, in.ID, in.SiteID, strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), strings.TrimSpace(in.Instructions), active)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "lobby.upsert", "lobby", in.ID, r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]string{"id": in.ID})
}

func (s *Server) upsertDepartment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		ParentID string `json:"parentId"`
		Color    string `json:"color"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "name_required", "부서명은 필수입니다")
		return
	}
	if in.ID == "" {
		in.ID = newID()
	}
	if in.Color == "" {
		in.Color = "#2F6B5F"
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO organizations(id,name,parent_id,color) VALUES($1,$2,NULLIF($3,''),$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,parent_id=EXCLUDED.parent_id,color=EXCLUDED.color,updated_at=now()`, in.ID, strings.TrimSpace(in.Name), in.ParentID, in.Color)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "department.upsert", "organization", in.ID, r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]string{"id": in.ID})
}

func (s *Server) listWatchlist(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,COALESCE(name_encrypted,''),COALESCE(company,''),reason_encrypted,starts_at,ends_at,active,created_at FROM watchlist_entries ORDER BY created_at DESC`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, nameEnc, company, reasonEnc string
		var starts, created time.Time
		var ends *time.Time
		var active bool
		if rows.Scan(&id, &nameEnc, &company, &reasonEnc, &starts, &ends, &active, &created) == nil {
			items = append(items, map[string]any{"id": id, "name": s.decryptOptional(nameEnc), "company": company, "reason": s.decryptOptional(reasonEnc), "startsAt": starts, "endsAt": ends, "active": active, "createdAt": created})
		}
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "watchlist.view", "watchlist", "", r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) createWatchlist(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string     `json:"name"`
		Phone   string     `json:"phone"`
		Company string     `json:"company"`
		Reason  string     `json:"reason"`
		EndsAt  *time.Time `json:"endsAt"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Reason) == "" || (normalizePhone(in.Phone) == "" && strings.TrimSpace(in.Company) == "") {
		writeError(w, 400, "required_fields", "전화번호 또는 회사와 제한 사유가 필요합니다")
		return
	}
	nameEnc, _ := s.encryptOptional(in.Name)
	reasonEnc, err := s.keys.Encrypt(strings.TrimSpace(in.Reason))
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	var phoneHash []byte
	if normalizePhone(in.Phone) != "" {
		phoneHash = s.keys.Digest("phone:" + normalizePhone(in.Phone))
	}
	id := newID()
	u, _ := userFrom(r)
	_, err = s.db.Exec(r.Context(), `INSERT INTO watchlist_entries(id,name_encrypted,phone_hash,company,reason_encrypted,ends_at,created_by) VALUES($1,NULLIF($2,''),$3,NULLIF($4,''),$5,$6,$7)`, id, nameEnc, phoneHash, strings.TrimSpace(in.Company), reasonEnc, in.EndsAt, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "watchlist.create", "watchlist", id, r.RemoteAddr, map[string]any{"company": in.Company, "phoneConfigured": len(phoneHash) > 0})
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Server) deleteWatchlist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "entryID")
	tag, err := s.db.Exec(r.Context(), `UPDATE watchlist_entries SET active=false WHERE id=$1 AND active`, id)
	if err != nil || tag.RowsAffected() == 0 {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "watchlist.disable", "watchlist", id, r.RemoteAddr, nil)
	w.WriteHeader(http.StatusNoContent)
}
