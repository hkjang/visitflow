package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/database"
)

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	queries := map[string]string{
		"today":           `SELECT count(*) FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites s ON s.id=v.site_id WHERE (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date AND vv.status NOT IN ('CANCELLED','REJECTED')`,
		"current":         `SELECT count(*) FROM visitor_visits WHERE status='CHECKED_IN'`,
		"pendingApproval": `SELECT count(*) FROM visits WHERE status='PENDING_APPROVAL'`,
		"noShow":          `SELECT count(*) FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites s ON s.id=v.site_id WHERE (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date AND vv.status='NO_SHOW'`,
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

// statisticsTodayCTE anchors the daily trend to the newest calendar day any
// site is currently in. The trend dates each participation in its own site's
// timezone but used to lay those buckets on an axis of CURRENT_DATE, which is
// the database session's date — UTC in the shipped container. On the default
// Asia/Seoul site that axis stops nine hours short, so from midnight until
// 09:00 local the day the dashboard counts as "오늘" has no column at all and
// every visit booked for it disappears from the chart and the CSV. Taking the
// latest site-local date keeps each site's current day on the axis.
const statisticsTodayCTE = `today AS (SELECT COALESCE(max((now() AT TIME ZONE si.timezone)::date),CURRENT_DATE) AS day FROM sites si)`

// statisticsSpanCTE names the exact calendar the trend draws: the requested
// number of site-local days ending on the newest day any site is in. Every
// figure on the statistics screen takes $1 as the day count and reads its
// window from here.
const statisticsSpanCTE = statisticsTodayCTE + `, span AS (SELECT (SELECT day FROM today)-($1::int-1) AS from_day,(SELECT day FROM today) AS to_day)`

// statisticsSpanWhere restricts a timestamptz column to the span. The summary
// tiles and the breakdowns used to filter on CURRENT_DATE-days, which is the
// database session's midnight — UTC in the shipped container — so on the
// default Asia/Seoul site they counted from 09:00 on the day before the chart's
// first column. The tiles therefore reported more participants than the trend
// beside them drew, and the extra ones came from a day with no bar at all.
// The two plain comparisons keep the timestamp indexes usable; the site-local
// date test then trims the day of slack a timezone offset can add.
func statisticsSpanWhere(column string) string {
	return column + `>=((SELECT from_day FROM span)-1)::timestamp AND ` + column + `<((SELECT to_day FROM span)+2)::timestamp` +
		` AND (` + column + ` AT TIME ZONE si.timezone)::date BETWEEN (SELECT from_day FROM span) AND (SELECT to_day FROM span)`
}

func (s *Server) statistics(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 366 {
		days = 30
	}
	// Bucket each participation into its site-local day once, then join the
	// buckets to the calendar; the previous per-day correlated subquery scanned
	// every visit for every day in the range.
	rows, err := s.db.Query(r.Context(), `WITH `+statisticsSpanCTE+`, buckets AS (
		SELECT (v.start_at AT TIME ZONE si.timezone)::date AS day,
			count(vv.id) AS scheduled,
			count(vv.id) FILTER(WHERE vv.status IN ('CHECKED_IN','CHECKED_OUT')) AS checked
		FROM visits v JOIN sites si ON si.id=v.site_id JOIN visitor_visits vv ON vv.visit_id=v.id
		WHERE `+statisticsSpanWhere("v.start_at")+`
		GROUP BY 1)
		SELECT d::date,COALESCE(b.scheduled,0),COALESCE(b.checked,0)
		FROM generate_series((SELECT from_day FROM span),(SELECT to_day FROM span),interval '1 day') d LEFT JOIN buckets b ON b.day=d::date ORDER BY d`, days)
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
	rows, err = s.db.Query(r.Context(), `WITH `+statisticsSpanCTE+`
		SELECT COALESCE(o.name,'미지정'),count(vv.id) FROM visits v JOIN sites si ON si.id=v.site_id JOIN visitor_visits vv ON vv.visit_id=v.id LEFT JOIN organizations o ON o.id=v.department_id
		WHERE `+statisticsSpanWhere("v.start_at")+` GROUP BY o.name ORDER BY count(vv.id) DESC LIMIT 20`, days)
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
	group := func(query string) []map[string]any {
		items := []map[string]any{}
		rows, err := s.db.Query(r.Context(), query, days)
		if err != nil {
			return items
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			var count int
			if rows.Scan(&name, &count) == nil {
				items = append(items, map[string]any{"name": name, "count": count})
			}
		}
		return items
	}
	bySite := group(`WITH ` + statisticsSpanCTE + `
		SELECT si.name,count(vv.id) FROM visits v JOIN sites si ON si.id=v.site_id JOIN visitor_visits vv ON vv.visit_id=v.id
		WHERE ` + statisticsSpanWhere("v.start_at") + ` GROUP BY si.name ORDER BY count(vv.id) DESC LIMIT 20`)
	byVisitType := group(`WITH ` + statisticsSpanCTE + `
		SELECT COALESCE(vt.name,'미지정'),count(vv.id) FROM visits v JOIN sites si ON si.id=v.site_id LEFT JOIN visit_types vt ON vt.id=v.visit_type_id JOIN visitor_visits vv ON vv.visit_id=v.id
		WHERE ` + statisticsSpanWhere("v.start_at") + ` GROUP BY vt.name ORDER BY count(vv.id) DESC LIMIT 20`)
	byHour := group(`WITH ` + statisticsSpanCTE + `
		SELECT lpad(extract(hour FROM (vv.checked_in_at AT TIME ZONE si.timezone))::int::text,2,'0'),count(*) FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites si ON si.id=v.site_id
		WHERE vv.checked_in_at IS NOT NULL AND ` + statisticsSpanWhere("vv.checked_in_at") + ` GROUP BY 1 ORDER BY 1`)
	bySource := group(`WITH ` + statisticsSpanCTE + `
		SELECT v.source,count(*) FROM visits v JOIN sites si ON si.id=v.site_id
		WHERE ` + statisticsSpanWhere("v.start_at") + ` GROUP BY v.source ORDER BY count(*) DESC`)
	var totalParticipants, checkedIn, noShow, cancelled, selfRegistered int
	var avgDwellMinutes, avgLeadHours float64
	_ = s.db.QueryRow(r.Context(), `WITH `+statisticsSpanCTE+`
		SELECT count(vv.id),
		count(vv.id) FILTER(WHERE vv.status IN ('CHECKED_IN','CHECKED_OUT')),
		count(vv.id) FILTER(WHERE vv.status='NO_SHOW'),
		count(vv.id) FILTER(WHERE vv.status='CANCELLED'),
		count(vv.id) FILTER(WHERE EXISTS(SELECT 1 FROM consent_records c WHERE c.visitor_visit_id=vv.id AND c.source='self')),
		COALESCE(avg(EXTRACT(EPOCH FROM vv.checked_out_at-vv.checked_in_at)/60) FILTER(WHERE vv.checked_out_at IS NOT NULL AND vv.checked_in_at IS NOT NULL),0),
		COALESCE(avg(EXTRACT(EPOCH FROM v.start_at-v.created_at)/3600) FILTER(WHERE v.start_at>v.created_at),0)
		FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites si ON si.id=v.site_id WHERE `+statisticsSpanWhere("v.start_at"), days).
		Scan(&totalParticipants, &checkedIn, &noShow, &cancelled, &selfRegistered, &avgDwellMinutes, &avgLeadHours)
	writeJSON(w, 200, map[string]any{
		"days": days, "daily": daily, "byDepartment": byDepartment, "bySite": bySite, "byVisitType": byVisitType, "byHour": byHour, "bySource": bySource,
		"summary": map[string]any{"participants": totalParticipants, "checkedIn": checkedIn, "noShow": noShow, "cancelled": cancelled, "selfRegistered": selfRegistered,
			"avgDwellMinutes": int(avgDwellMinutes + 0.5), "avgLeadHours": int(avgLeadHours + 0.5)},
	})
}

// auditLogFilters is the audit screen's filter set. The on-screen list and the
// CSV export parse it through the same code so a download always contains the
// rows the auditor narrowed to; an export that quietly widens back to every
// actor and date is worse than no export in a compliance review.
type auditLogFilters struct {
	action string
	actor  string
	from   *time.Time
	to     *time.Time
}

func parseAuditLogFilters(r *http.Request) auditLogFilters {
	filters := auditLogFilters{
		action: strings.TrimSpace(r.URL.Query().Get("action")),
		actor:  strings.TrimSpace(r.URL.Query().Get("actor")),
	}
	if value := strings.TrimSpace(r.URL.Query().Get("from")); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			filters.from = &parsed
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("to")); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			filters.to = &parsed
		}
	}
	return filters
}

// details records what the export actually covered, so the audit trail of an
// export names its scope instead of implying the whole log was taken.
func (f auditLogFilters) details() map[string]any {
	out := map[string]any{"action": f.action, "actor": f.actor}
	if f.from != nil {
		out["from"] = f.from.Format(time.RFC3339)
	}
	if f.to != nil {
		out["to"] = f.to.Format(time.RFC3339)
	}
	return out
}

func (s *Server) auditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 200
	}
	filters := parseAuditLogFilters(r)
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	// Keyset on the bigserial id keeps paging stable while new rows arrive.
	rows, err := s.db.Query(r.Context(), `SELECT a.id,COALESCE(u.display_name,'system'),a.action,a.resource_type,COALESCE(a.resource_id,''),COALESCE(a.ip_address,''),a.details,a.created_at
		FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id
		WHERE ($1='' OR a.action ILIKE $1||'%')
		AND ($3='' OR u.display_name ILIKE '%'||$3||'%' OR u.username ILIKE '%'||$3||'%')
		AND ($4::bigint=0 OR a.id<$4)
		AND ($5::timestamptz IS NULL OR a.created_at>=$5)
		AND ($6::timestamptz IS NULL OR a.created_at<=$6)
		ORDER BY a.id DESC LIMIT $2`, filters.action, limit+1, filters.actor, before, filters.from, filters.to)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var actorName, act, resourceType, resourceID, ip string
		var details []byte
		var created time.Time
		if rows.Scan(&id, &actorName, &act, &resourceType, &resourceID, &ip, &details, &created) == nil {
			var safe any
			_ = json.Unmarshal(details, &safe)
			items = append(items, map[string]any{"id": id, "actor": actorName, "action": act, "resourceType": resourceType, "resourceId": resourceID, "ipAddress": ip, "details": safe, "createdAt": created})
		}
	}
	nextBefore := int64(0)
	if len(items) > limit {
		items = items[:limit]
		nextBefore, _ = items[len(items)-1]["id"].(int64)
	}
	writeJSON(w, 200, map[string]any{"items": items, "hasMore": nextBefore > 0, "nextBefore": nextBefore})
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 1000 {
		limit = 300
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	rows, err := s.db.Query(r.Context(), `SELECT n.id,n.visit_id,n.channel,n.template_key,n.status,n.attempts,COALESCE(n.error,''),n.created_at,n.sent_at,n.recipient_encrypted,
		COALESCE(na.name,'기존 Adapter'),COALESCE(nr.name,''),n.next_attempt_at
		FROM notifications n LEFT JOIN notification_api_configs na ON na.id=n.api_config_id LEFT JOIN notification_rules nr ON nr.id=n.rule_id
		WHERE ($1='' OR n.status=$1)
		ORDER BY n.created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, channel, key, status, errorText, recipientEnc, apiName, ruleName string
		var visitID *string
		var attempts int
		var created, nextAttempt time.Time
		var sent *time.Time
		if rows.Scan(&id, &visitID, &channel, &key, &status, &attempts, &errorText, &created, &sent, &recipientEnc, &apiName, &ruleName, &nextAttempt) == nil {
			recipient := maskPhone(s.decryptOptional(recipientEnc))
			if channel == "webhook" {
				recipient = "외부 시스템"
			} else if channel == "email" {
				recipient = maskEmail(s.decryptOptional(recipientEnc))
			}
			items = append(items, map[string]any{"id": id, "visitId": visitID, "channel": channel, "templateKey": key, "status": status, "attempts": attempts, "error": errorText, "recipient": recipient, "apiConfigName": apiName, "ruleName": ruleName, "createdAt": created, "sentAt": sent, "nextAttemptAt": nextAttempt})
		}
	}
	if err := rows.Err(); err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) listVisitors(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 1000 {
		limit = 300
	}
	// Names and phones are encrypted; search matches their HMAC indexes exactly,
	// while the company column supports a substring match.
	rows, err := s.db.Query(r.Context(), `SELECT p.id,p.name_encrypted,p.phone_encrypted,COALESCE(p.company,''),count(vv.id),max(v.start_at),p.masked_at,p.erased_at
		FROM visitors p LEFT JOIN visitor_visits vv ON vv.visitor_id=p.id LEFT JOIN visits v ON v.id=vv.visit_id
		WHERE ($1='' OR p.company ILIKE '%'||$1||'%' OR p.name_hash=$2 OR p.phone_hash=$3)
		GROUP BY p.id ORDER BY max(v.start_at) DESC NULLS LAST LIMIT $4`,
		search, s.keys.Digest("name:"+strings.ToLower(search)), s.keys.Digest("phone:"+normalizePhone(search)), limit)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, nameEnc, phoneEnc, company string
		var visits int
		var lastVisit, masked, erased *time.Time
		if rows.Scan(&id, &nameEnc, &phoneEnc, &company, &visits, &lastVisit, &masked, &erased) == nil {
			name := s.decryptOptional(nameEnc)
			if masked != nil {
				name = maskName(name)
			}
			items = append(items, map[string]any{"id": id, "name": name, "phone": maskPhone(s.decryptOptional(phoneEnc)), "company": company, "visitCount": visits, "lastVisitAt": lastVisit, "maskedAt": masked, "erasedAt": erased})
		}
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visitor.list", "visitor", "", clientIP(r), map[string]int{"count": len(items)})
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) upsertSite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Code     string `json:"code"`
		Name     string `json:"name"`
		Address  string `json:"address"`
		MapURL   string `json:"mapUrl"`
		Timezone string `json:"timezone"`
		Active   *bool  `json:"active"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "required_fields", "사업장 코드와 이름은 필수입니다")
		return
	}
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.Timezone == "" {
		in.Timezone = "Asia/Seoul"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil || in.Timezone == "Local" {
		writeError(w, 400, "invalid_timezone", "IANA 시간대 이름을 입력하세요. 예: Asia/Seoul")
		return
	}
	if in.ID == "" {
		in.ID = newID()
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO sites(id,code,name,address,map_url,timezone,active) VALUES($1,upper($2),$3,NULLIF($4,''),NULLIF($5,''),$6,$7) ON CONFLICT(id) DO UPDATE SET code=EXCLUDED.code,name=EXCLUDED.name,address=EXCLUDED.address,map_url=EXCLUDED.map_url,timezone=EXCLUDED.timezone,active=EXCLUDED.active,updated_at=now()`, in.ID, strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), strings.TrimSpace(in.Address), strings.TrimSpace(in.MapURL), in.Timezone, active)
	if database.IsConstraint(err, "sites_code_key") {
		writeError(w, http.StatusConflict, "duplicate_code", "이미 사용 중인 사업장 코드입니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "site.upsert", "site", in.ID, clientIP(r), map[string]string{"code": in.Code, "name": in.Name})
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
	if database.IsConstraint(err, "lobbies_site_id_code_key") {
		writeError(w, http.StatusConflict, "duplicate_code", "같은 사업장에 이미 있는 로비 코드입니다")
		return
	}
	if database.IsConstraint(err, "lobbies_site_id_fkey") {
		writeError(w, http.StatusBadRequest, "unknown_site", "사업장을 찾을 수 없습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "lobby.upsert", "lobby", in.ID, clientIP(r), nil)
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
	if in.ParentID != "" {
		if in.ParentID == in.ID {
			writeError(w, 400, "invalid_parent", "상위 조직으로 자기 자신을 지정할 수 없습니다")
			return
		}
		var exists bool
		if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM organizations WHERE id=$1)`, in.ParentID).Scan(&exists); err != nil || !exists {
			writeError(w, 400, "invalid_parent", "상위 조직을 찾을 수 없습니다")
			return
		}
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO organizations(id,name,parent_id,color) VALUES($1,$2,NULLIF($3,''),$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,parent_id=EXCLUDED.parent_id,color=EXCLUDED.color,updated_at=now()`, in.ID, strings.TrimSpace(in.Name), in.ParentID, in.Color)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "department.upsert", "organization", in.ID, clientIP(r), nil)
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
	s.audit(r.Context(), u.ID, "watchlist.view", "watchlist", "", clientIP(r), nil)
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
	if in.EndsAt != nil && !in.EndsAt.After(time.Now()) {
		writeError(w, 400, "invalid_period", "제한 종료일은 현재 이후여야 합니다")
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
	s.audit(r.Context(), u.ID, "watchlist.create", "watchlist", id, clientIP(r), map[string]any{"company": in.Company, "phoneConfigured": len(phoneHash) > 0})
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Server) deleteWatchlist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "entryID")
	tag, err := s.db.Exec(r.Context(), `UPDATE watchlist_entries SET active=false WHERE id=$1 AND active`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "활성 Watch List 항목이 없습니다")
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "watchlist.disable", "watchlist", id, clientIP(r), nil)
	w.WriteHeader(http.StatusNoContent)
}
