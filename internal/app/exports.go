package app

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// csvWriter emits a UTF-8 BOM so Excel on Windows opens Korean exports without
// a manual encoding step, which is how these files are actually read.
func csvWriter(w http.ResponseWriter, filename string) *csv.Writer {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	return csv.NewWriter(w)
}

func exportFilename(prefix string) string {
	return fmt.Sprintf("%s-%s.csv", prefix, time.Now().Format("20060102-1504"))
}

// formatTime renders with an explicit offset: the service container usually
// runs in UTC, and a bare wall-clock time would be misread by the operator.
func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

// formatSiteTime renders in the site's own timezone for human reading.
func formatSiteTime(value *time.Time, location *time.Location) string {
	if value == nil {
		return ""
	}
	return value.In(location).Format("2006-01-02 15:04")
}

func siteLocation(name string) *time.Location {
	if location, err := time.LoadLocation(name); err == nil {
		return location
	}
	return time.UTC
}

func (s *Server) exportAuditLogsCSV(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100000 {
		limit = 10000
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	rows, err := s.db.Query(r.Context(), `SELECT a.id,a.created_at,COALESCE(u.display_name,'system'),a.action,a.resource_type,COALESCE(a.resource_id,''),COALESCE(a.ip_address,''),a.details::text
		FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id
		WHERE ($1='' OR a.action ILIKE $1||'%') ORDER BY a.created_at DESC LIMIT $2`, action, limit)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	writer := csvWriter(w, exportFilename("visitflow-audit"))
	_ = writer.Write([]string{"id", "createdAt", "actor", "action", "resourceType", "resourceId", "ipAddress", "details"})
	count := 0
	for rows.Next() {
		var id int64
		var createdAt time.Time
		var actor, act, resourceType, resourceID, ip, details string
		if rows.Scan(&id, &createdAt, &actor, &act, &resourceType, &resourceID, &ip, &details) != nil {
			continue
		}
		_ = writer.Write([]string{strconv.FormatInt(id, 10), formatTime(&createdAt), actor, act, resourceType, resourceID, ip, details})
		count++
	}
	writer.Flush()
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "audit.export", "audit_log", "", r.RemoteAddr, map[string]any{"count": count, "action": action})
}

func (s *Server) exportVisitsCSV(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 3650 {
		days = 90
	}
	rows, err := s.db.Query(r.Context(), `SELECT v.request_no,v.start_at,v.end_at,si.name,si.timezone,COALESCE(l.name,''),COALESCE(vt.name,''),h.display_name,COALESCE(o.name,''),
		v.purpose,COALESCE(v.place_detail,''),v.status,vv.status,p.name_encrypted,COALESCE(p.company,''),p.masked_at,vv.checked_in_at,vv.checked_out_at,COALESCE(vv.badge_no,'')
		FROM visits v
		JOIN visitor_visits vv ON vv.visit_id=v.id
		JOIN visitors p ON p.id=vv.visitor_id
		JOIN users h ON h.id=v.host_user_id
		JOIN sites si ON si.id=v.site_id
		LEFT JOIN lobbies l ON l.id=v.lobby_id
		LEFT JOIN organizations o ON o.id=v.department_id
		LEFT JOIN visit_types vt ON vt.id=v.visit_type_id
		WHERE v.start_at>=now()-($1::int*interval '1 day')
		ORDER BY v.start_at DESC LIMIT 50000`, days)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	writer := csvWriter(w, exportFilename("visitflow-visits"))
	_ = writer.Write([]string{"requestNo", "startAt", "endAt", "timezone", "site", "lobby", "visitType", "host", "department", "purpose", "placeDetail", "visitStatus", "visitorStatus", "visitor", "company", "checkedInAt", "checkedOutAt", "badgeNo"})
	count := 0
	for rows.Next() {
		var requestNo, site, timezone, lobby, visitType, host, department, purpose, place, visitStatus, participantStatus, nameEnc, company, badge string
		var startAt, endAt time.Time
		var maskedAt, checkedIn, checkedOut *time.Time
		if rows.Scan(&requestNo, &startAt, &endAt, &site, &timezone, &lobby, &visitType, &host, &department, &purpose, &place, &visitStatus, &participantStatus, &nameEnc, &company, &maskedAt, &checkedIn, &checkedOut, &badge) != nil {
			continue
		}
		visitor := s.decryptOptional(nameEnc)
		if maskedAt != nil {
			visitor = maskName(visitor)
		}
		location := siteLocation(timezone)
		_ = writer.Write([]string{requestNo, formatSiteTime(&startAt, location), formatSiteTime(&endAt, location), timezone, site, lobby, visitType, host, department, purpose, place,
			visitStatus, participantStatus, visitor, company, formatSiteTime(checkedIn, location), formatSiteTime(checkedOut, location), badge})
		count++
	}
	writer.Flush()
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visit.export", "visit", "", r.RemoteAddr, map[string]any{"count": count, "days": days})
}

func (s *Server) exportStatisticsCSV(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 366 {
		days = 30
	}
	rows, err := s.db.Query(r.Context(), `SELECT d::date,COALESCE(count(DISTINCT vv.id),0),
		COALESCE(count(DISTINCT vv.id) FILTER(WHERE vv.status IN ('CHECKED_IN','CHECKED_OUT')),0),
		COALESCE(count(DISTINCT vv.id) FILTER(WHERE vv.status='NO_SHOW'),0),
		COALESCE(count(DISTINCT vv.id) FILTER(WHERE vv.status='CANCELLED'),0)
		FROM generate_series(CURRENT_DATE-($1::int-1),CURRENT_DATE,interval '1 day') d
		LEFT JOIN visits v ON v.start_at::date=d::date
		LEFT JOIN visitor_visits vv ON vv.visit_id=v.id
		GROUP BY d ORDER BY d`, days)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	writer := csvWriter(w, exportFilename("visitflow-statistics"))
	_ = writer.Write([]string{"date", "scheduled", "checkedIn", "noShow", "cancelled"})
	for rows.Next() {
		var day time.Time
		var scheduled, checkedIn, noShow, cancelled int
		if rows.Scan(&day, &scheduled, &checkedIn, &noShow, &cancelled) != nil {
			continue
		}
		_ = writer.Write([]string{day.Format("2006-01-02"), strconv.Itoa(scheduled), strconv.Itoa(checkedIn), strconv.Itoa(noShow), strconv.Itoa(cancelled)})
	}
	writer.Flush()
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "statistics.export", "statistics", "", r.RemoteAddr, map[string]any{"days": days})
}
