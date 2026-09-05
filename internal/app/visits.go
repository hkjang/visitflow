package app

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/skip2/go-qrcode"
)

func normalizePhone(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func maskPhone(v string) string {
	n := normalizePhone(v)
	if len(n) < 7 {
		return "***"
	}
	return n[:3] + "-****-" + n[len(n)-4:]
}

func maskName(v string) string {
	r := []rune(strings.TrimSpace(v))
	if len(r) <= 1 {
		return v
	}
	if len(r) == 2 {
		return string(r[0]) + "○"
	}
	return string(r[0]) + strings.Repeat("○", len(r)-2) + string(r[len(r)-1])
}

func siteAllowed(u User, siteID string) bool {
	if u.Role != RoleLobby || len(u.SiteScope) == 0 {
		return true
	}
	return containsString(u.SiteScope, siteID)
}

func (s *Server) encryptOptional(v string) (string, error) {
	if strings.TrimSpace(v) == "" {
		return "", nil
	}
	return s.keys.Encrypt(strings.TrimSpace(v))
}

func (s *Server) decryptOptional(v string) string {
	if v == "" {
		return ""
	}
	plain, err := s.keys.Decrypt(v)
	if err != nil {
		s.logger.Warn("personal data decrypt failed", "error", err)
		return ""
	}
	return plain
}

func (s *Server) referenceData(w http.ResponseWriter, r *http.Request) {
	sites := []map[string]any{}
	rows, err := s.db.Query(r.Context(), `SELECT id,code,name,COALESCE(address,''),COALESCE(map_url,''),timezone FROM sites WHERE active ORDER BY name`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	for rows.Next() {
		var id, code, name, address, mapURL, timezone string
		if rows.Scan(&id, &code, &name, &address, &mapURL, &timezone) == nil {
			sites = append(sites, map[string]any{"id": id, "code": code, "name": name, "address": address, "mapUrl": mapURL, "timezone": timezone})
		}
	}
	rows.Close()
	lobbies := []map[string]any{}
	rows, err = s.db.Query(r.Context(), `SELECT id,site_id,code,name,COALESCE(instructions,'') FROM lobbies WHERE active ORDER BY name`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	for rows.Next() {
		var id, siteID, code, name, instructions string
		if rows.Scan(&id, &siteID, &code, &name, &instructions) == nil {
			lobbies = append(lobbies, map[string]any{"id": id, "siteId": siteID, "code": code, "name": name, "instructions": instructions})
		}
	}
	rows.Close()
	departments := []map[string]any{}
	rows, err = s.db.Query(r.Context(), `SELECT id,name,parent_id,color FROM organizations ORDER BY name`)
	if err == nil {
		for rows.Next() {
			var id, name, color string
			var parent *string
			if rows.Scan(&id, &name, &parent, &color) == nil {
				departments = append(departments, map[string]any{"id": id, "name": name, "parentId": parent, "color": color})
			}
		}
		rows.Close()
	}
	hosts := []map[string]any{}
	rows, err = s.db.Query(r.Context(), `SELECT id,display_name,COALESCE(email,''),department_id FROM users WHERE active ORDER BY display_name LIMIT 1000`)
	if err == nil {
		for rows.Next() {
			var id, name, email string
			var departmentID *string
			if rows.Scan(&id, &name, &email, &departmentID) == nil {
				hosts = append(hosts, map[string]any{"id": id, "name": name, "email": email, "departmentId": departmentID})
			}
		}
		rows.Close()
	}
	visitTypes, err := s.visitTypes(r.Context(), true)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sites": sites, "lobbies": lobbies, "departments": departments, "hosts": hosts,
		"visitTypes": visitTypes, "locales": s.supportedLocales(r.Context()), "defaultLocale": s.defaultLocale(r.Context()),
		"selfRegistrationEnabled": settingOr(s, r.Context(), "visit.self_registration_enabled", "true") == "true",
	})
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	// Department and delegation are pointers so a client that omits them keeps
	// the stored values; only an explicit empty string clears a field. Phone is
	// intentionally replaced wholesale because the UI documents that behaviour.
	var in struct {
		DisplayName    string  `json:"displayName"`
		Phone          *string `json:"phone"`
		DepartmentID   *string `json:"departmentId"`
		DelegateUserID *string `json:"delegateUserId"`
		DelegateUntil  string  `json:"delegateUntil"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	u, _ := userFrom(r)
	// An omitted phone keeps the stored number; an explicit "" clears it. The
	// old whole-replace behaviour wiped the host's number on every name change.
	phoneSet, phone := in.Phone != nil, ""
	if phoneSet {
		if normalized := normalizePhone(*in.Phone); normalized != "" && len(normalized) < 7 {
			writeError(w, http.StatusBadRequest, "invalid_phone", "휴대전화 번호를 확인하세요")
			return
		}
		encrypted, err := s.encryptOptional(*in.Phone)
		if err != nil {
			notFoundOrServer(w, err)
			return
		}
		phone = encrypted
	}
	var err error
	departmentSet, department := in.DepartmentID != nil, ""
	if departmentSet {
		department = strings.TrimSpace(*in.DepartmentID)
	}
	delegateSet, delegateID := in.DelegateUserID != nil, ""
	if delegateSet {
		delegateID = strings.TrimSpace(*in.DelegateUserID)
	}
	var delegateUntil *time.Time
	if delegateID != "" {
		if delegateID == u.ID {
			writeError(w, http.StatusBadRequest, "invalid_delegate", "본인을 대리자로 지정할 수 없습니다")
			return
		}
		var active bool
		if err := s.db.QueryRow(r.Context(), `SELECT active FROM users WHERE id=$1`, delegateID).Scan(&active); err != nil || !active {
			writeError(w, http.StatusBadRequest, "invalid_delegate", "대리자로 지정할 수 있는 사용자가 아닙니다")
			return
		}
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(in.DelegateUntil))
		if parseErr != nil || !parsed.After(time.Now()) {
			writeError(w, http.StatusBadRequest, "invalid_delegate_period", "대리 지정 종료 시각은 현재 이후 값이어야 합니다")
			return
		}
		if parsed.After(time.Now().AddDate(1, 0, 0)) {
			writeError(w, http.StatusBadRequest, "invalid_delegate_period", "대리 지정은 최대 1년까지 설정할 수 있습니다")
			return
		}
		delegateUntil = &parsed
	}
	_, err = s.db.Exec(r.Context(), `UPDATE users SET display_name=COALESCE(NULLIF($2,''),display_name),
		phone_encrypted=CASE WHEN $9 THEN NULLIF($3,'') ELSE phone_encrypted END,
		department_id=CASE WHEN $4 THEN NULLIF($5,'') ELSE department_id END,
		delegate_user_id=CASE WHEN $6 THEN NULLIF($7,'') ELSE delegate_user_id END,
		delegate_until=CASE WHEN $6 THEN $8 ELSE delegate_until END,
		updated_at=now() WHERE id=$1`, u.ID, strings.TrimSpace(in.DisplayName), phone, departmentSet, department, delegateSet, delegateID, delegateUntil, phoneSet)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "profile.update", "user", u.ID, clientIP(r), map[string]any{
		"departmentChanged": departmentSet, "phoneChanged": phoneSet, "phoneConfigured": phone != "",
		"delegateChanged": delegateSet, "delegateUserId": delegateID, "delegateUntil": delegateUntil,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) personalDashboard(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	counts := map[string]int{}
	queries := map[string]string{
		"today":    `SELECT count(*) FROM visits v JOIN sites s ON s.id=v.site_id WHERE v.host_user_id=$1 AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date AND v.status NOT IN ('CANCELLED','REJECTED')`,
		"upcoming": `SELECT count(*) FROM visits v WHERE v.host_user_id=$1 AND v.start_at>now() AND v.status IN ('PENDING_APPROVAL','APPROVED','SCHEDULED')`,
		"arrived":  `SELECT count(*) FROM visits v JOIN sites s ON s.id=v.site_id WHERE v.host_user_id=$1 AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date AND v.status IN ('ARRIVED','CHECKED_IN')`,
		"pending":  `SELECT count(*) FROM visits v WHERE v.host_user_id=$1 AND v.status='PENDING_APPROVAL'`,
	}
	for k, q := range queries {
		var count int
		if err := s.db.QueryRow(r.Context(), q, u.ID).Scan(&count); err != nil {
			notFoundOrServer(w, err)
			return
		}
		counts[k] = count
	}
	items, _, err := s.queryVisits(r.Context(), u, visitQuery{Period: "today", Limit: 8})
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts, "items": items})
}

// visitQuery describes one page of the visit directory. Cursor keeps paging
// stable while new visits are created, which a plain offset cannot do.
type visitQuery struct {
	Status string
	Period string
	Search string
	// ID restricts the result to one visit by id or request number, bypassing
	// the substring search a detail lookup does not need.
	ID     string
	Cursor string
	Limit  int
}

// encodeVisitCursor and decodeVisitCursor carry the keyset position
// (start_at, id) that the next page continues from.
func encodeVisitCursor(item VisitSummary) string {
	return base64.RawURLEncoding.EncodeToString([]byte(item.StartAt.UTC().Format(time.RFC3339Nano) + "|" + item.ID))
}

func decodeVisitCursor(cursor string) (time.Time, string, bool) {
	if cursor == "" {
		return time.Time{}, "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	timestamp, id, found := strings.Cut(string(raw), "|")
	if !found || id == "" {
		return time.Time{}, "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return time.Time{}, "", false
	}
	return parsed, id, true
}

func (s *Server) listVisits(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 100
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if cursor != "" {
		if _, _, ok := decodeVisitCursor(cursor); !ok {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "페이지 커서가 올바르지 않습니다")
			return
		}
	}
	items, nextCursor, err := s.queryVisits(r.Context(), u, visitQuery{
		Status: r.URL.Query().Get("status"), Period: r.URL.Query().Get("period"),
		Search: r.URL.Query().Get("q"), Cursor: cursor, Limit: limit,
	})
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor, "hasMore": nextCursor != ""})
}

// queryVisits returns one page plus the cursor for the next one. It fetches a
// single extra row to decide whether more pages exist without a count query.
//
// The participant part of the search — company, visitor name and phone — is an
// EXISTS over the visit rather than a predicate on the joined participant rows.
// Those rows are what count(vv.id) and the primary-visitor array_agg below are
// built from, so filtering them threw away every companion who did not match
// the term: a search for one visitor's phone number reported "방문자 1명" for a
// party of five and named the matching visitor as the representative instead of
// the primary one. The subquery decides whether the visit matches and leaves
// the aggregate looking at the whole party.
func (s *Server) queryVisits(ctx context.Context, u User, q visitQuery) ([]VisitSummary, string, error) {
	dept := ""
	if u.DepartmentID != nil {
		dept = *u.DepartmentID
	}
	if q.Limit < 1 {
		q.Limit = 100
	}
	cursorTime, cursorID, hasCursor := decodeVisitCursor(q.Cursor)
	search := strings.TrimSpace(q.Search)
	rows, err := s.db.Query(ctx, `
		SELECT v.id,v.request_no,v.host_user_id,h.display_name,v.department_id,COALESCE(o.name,''),v.site_id,s.name,v.lobby_id,COALESCE(l.name,''),
		v.start_at,v.end_at,v.purpose,COALESCE(v.place_detail,''),v.status,v.source,v.created_at,v.visit_type_id,COALESCE(vt.name,''),COALESCE(v.approval_reason,''),COALESCE(v.recurrence->>'seriesId',''),
		count(vv.id),COALESCE((array_agg(p.name_encrypted ORDER BY vv.is_primary DESC,vv.created_at))[1],''),COALESCE((array_agg(p.company ORDER BY vv.is_primary DESC,vv.created_at))[1],''),(array_agg(p.masked_at ORDER BY vv.is_primary DESC,vv.created_at))[1]
		FROM visits v JOIN users h ON h.id=v.host_user_id JOIN sites s ON s.id=v.site_id
		LEFT JOIN organizations o ON o.id=v.department_id LEFT JOIN lobbies l ON l.id=v.lobby_id
		LEFT JOIN visit_types vt ON vt.id=v.visit_type_id
		LEFT JOIN visitor_visits vv ON vv.visit_id=v.id LEFT JOIN visitors p ON p.id=vv.visitor_id
		WHERE ($1='' OR v.status=$1)
		AND ($2='' OR ($2='today' AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date) OR ($2='upcoming' AND v.start_at>=now()) OR ($2='past' AND v.end_at<now()))
		AND ($3='' OR v.id=$3 OR v.request_no ILIKE '%%'||$3||'%%' OR h.display_name ILIKE '%%'||$3||'%%'
		 OR EXISTS(SELECT 1 FROM visitor_visits mvv JOIN visitors mp ON mp.id=mvv.visitor_id
			WHERE mvv.visit_id=v.id AND (mp.company ILIKE '%%'||$3||'%%' OR mp.name_hash=$9 OR mp.phone_hash=$10)))
		AND ($5 IN ('admin','super_admin','security','auditor')
		 OR ($5='lobby' AND (cardinality($7::text[])=0 OR v.site_id=ANY($7::text[])))
		 OR ($5='dept_manager' AND v.department_id=NULLIF($6,''))
		 OR v.host_user_id=$4
		 OR EXISTS(SELECT 1 FROM users hu WHERE hu.id=v.host_user_id AND hu.delegate_user_id=$4 AND hu.delegate_until>now() AND hu.active)
		 OR EXISTS(SELECT 1 FROM users m WHERE m.delegate_user_id=$4 AND m.delegate_until>now() AND m.active AND m.role='dept_manager' AND m.department_id=v.department_id))
		AND ($11=false OR (v.start_at,v.id)<($12::timestamptz,$13))
		AND ($14='' OR v.id=$14 OR v.request_no=$14)
		GROUP BY v.id,h.display_name,o.name,s.name,l.name,vt.name ORDER BY v.start_at DESC,v.id DESC LIMIT $8`,
		q.Status, q.Period, search, u.ID, u.Role, dept, u.SiteScope, q.Limit+1,
		s.keys.Digest("name:"+strings.ToLower(search)), s.keys.Digest("phone:"+normalizePhone(search)),
		hasCursor, cursorTime, cursorID, strings.TrimSpace(q.ID))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []VisitSummary{}
	for rows.Next() {
		var item VisitSummary
		var nameEncrypted string
		var maskedAt *time.Time
		if err := rows.Scan(&item.ID, &item.RequestNo, &item.HostUserID, &item.HostName, &item.DepartmentID, &item.DepartmentName, &item.SiteID, &item.SiteName, &item.LobbyID, &item.LobbyName, &item.StartAt, &item.EndAt, &item.Purpose, &item.PlaceDetail, &item.Status, &item.Source, &item.CreatedAt, &item.VisitTypeID, &item.VisitTypeName, &item.ApprovalReason, &item.SeriesID, &item.VisitorCount, &nameEncrypted, &item.Company, &maskedAt); err != nil {
			return nil, "", err
		}
		item.PrimaryVisitor = s.decryptOptional(nameEncrypted)
		if maskedAt != nil {
			item.PrimaryVisitor = maskName(item.PrimaryVisitor)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(items) > q.Limit {
		items = items[:q.Limit]
		nextCursor = encodeVisitCursor(items[len(items)-1])
	}
	return items, nextCursor, nil
}

func (s *Server) createVisit(w http.ResponseWriter, r *http.Request) {
	var in VisitInput
	if !decodeJSON(w, r, &in) {
		return
	}
	u, _ := userFrom(r)
	result, err := s.createVisitRecord(r.Context(), r, u, in, "employee", false)
	if err != nil {
		writeVisitError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

type visitError struct {
	status  int
	code    string
	message string
}

func (e visitError) Error() string { return e.message }

func writeVisitError(w http.ResponseWriter, err error) {
	var ve visitError
	if errors.As(err, &ve) {
		writeError(w, ve.status, ve.code, ve.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "visit_failed", "방문 요청을 처리하지 못했습니다")
}

// requestNoSuffix draws the six characters a request number ends with. The
// suffix used to be the uppercased first six characters of a five-byte base64url
// token with "_" stripped out, but that token is only seven characters long: when
// two of them were separators fewer than six were left and the slice panicked, so
// roughly one visit in three hundred failed with a 500 the requester could only
// answer by submitting the form again. Twelve bytes leave a wide margin, and the
// retry makes running short impossible rather than merely unlikely.
func requestNoSuffix() (string, error) {
	separators := strings.NewReplacer("_", "", "-", "")
	for attempt := 0; attempt < 4; attempt++ {
		random, err := platform.RandomToken(12)
		if err != nil {
			return "", err
		}
		if cleaned := strings.ToUpper(separators.Replace(random)); len(cleaned) >= 6 {
			return cleaned[:6], nil
		}
	}
	return "", errors.New("방문 신청번호를 생성하지 못했습니다")
}

func (s *Server) createVisitRecord(ctx context.Context, r *http.Request, actor User, in VisitInput, source string, autoCheckIn bool) (map[string]any, error) {
	in.Purpose = strings.TrimSpace(in.Purpose)
	if in.SiteID == "" || in.Purpose == "" || len(in.Visitors) == 0 || len(in.Visitors) > 100 {
		return nil, visitError{400, "required_fields", "사업장, 방문 목적, 방문자 1~100명은 필수입니다"}
	}
	if in.StartAt.IsZero() || in.EndAt.IsZero() || !in.EndAt.After(in.StartAt) {
		return nil, visitError{400, "invalid_schedule", "방문 종료시간은 시작시간 이후여야 합니다"}
	}
	if in.EndAt.Sub(in.StartAt) > 31*24*time.Hour {
		return nil, visitError{400, "schedule_too_long", "한 방문 일정은 31일을 초과할 수 없습니다"}
	}
	if !siteAllowed(actor, in.SiteID) {
		return nil, visitError{403, "site_scope_forbidden", "담당 사업장 범위를 벗어난 요청입니다"}
	}
	if err := s.lobbyBelongsToSite(ctx, in.LobbyID, in.SiteID); err != nil {
		return nil, err
	}
	companyRequired, _ := s.getSetting(ctx, "visit.company_required")
	for _, visitor := range in.Visitors {
		if strings.TrimSpace(visitor.Name) == "" || len(normalizePhone(visitor.Phone)) < 7 || !visitor.Consent {
			return nil, visitError{400, "invalid_visitor", "방문자 이름, 휴대전화, 개인정보 동의는 필수입니다"}
		}
		if companyRequired == "true" && strings.TrimSpace(visitor.Company) == "" {
			return nil, visitError{400, "company_required", "회사명은 현재 정책상 필수입니다"}
		}
		var watchID string
		err := s.db.QueryRow(ctx, `SELECT id FROM watchlist_entries WHERE active AND starts_at<=now() AND (ends_at IS NULL OR ends_at>now()) AND (phone_hash=$1 OR (company<>'' AND lower(company)=lower($2))) LIMIT 1`, s.keys.Digest("phone:"+normalizePhone(visitor.Phone)), strings.TrimSpace(visitor.Company)).Scan(&watchID)
		if err == nil {
			s.audit(ctx, actor.ID, "watchlist.match", "watchlist", watchID, requestRemote(r), map[string]string{"source": source})
			return nil, visitError{403, "visit_restricted", "보안 정책에 따라 방문 등록을 완료할 수 없습니다. 보안 담당자에게 문의하세요"}
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	hostID := actor.ID
	if in.HostUserID != "" && (actor.CanManageLobby() || actor.IsAdmin()) {
		hostID = in.HostUserID
	}
	if hostID == "" {
		return nil, visitError{400, "host_required", "방문 담당자를 지정해야 합니다"}
	}
	var hostActive bool
	if err := s.db.QueryRow(ctx, `SELECT active FROM users WHERE id=$1`, hostID).Scan(&hostActive); err != nil || !hostActive {
		return nil, visitError{400, "invalid_host", "방문 담당자를 찾을 수 없거나 비활성 사용자입니다"}
	}
	if in.DepartmentID == "" {
		var d *string
		_ = s.db.QueryRow(ctx, `SELECT department_id FROM users WHERE id=$1`, hostID).Scan(&d)
		if d != nil {
			in.DepartmentID = *d
		}
	}
	visitType, checklistJSON, err := s.applyVisitType(ctx, &in)
	if err != nil {
		return nil, err
	}
	approval, _ := s.getSetting(ctx, "visit.approval_enabled")
	status := "SCHEDULED"
	participantStatus := "SCHEDULED"
	// A visit type may require approval even when the global workflow is off,
	// so that contractor work never bypasses review.
	if (approval == "true" || visitType.RequiresApproval) && source != "lobby" {
		status, participantStatus = "PENDING_APPROVAL", "PENDING_APPROVAL"
	}
	occurrences := 1
	if len(in.Recurrence) > 0 {
		if source == "lobby" || fmt.Sprint(in.Recurrence["frequency"]) != "weekly" {
			return nil, visitError{400, "invalid_recurrence", "반복 예약은 일반 방문의 매주 반복만 지원합니다"}
		}
		switch value := in.Recurrence["occurrences"].(type) {
		case float64:
			occurrences = int(value)
		case int:
			occurrences = value
		case json.Number:
			occurrences, _ = strconv.Atoi(value.String())
		default:
			occurrences = 0
		}
		if occurrences < 2 || occurrences > 52 || occurrences*len(in.Visitors) > 500 {
			return nil, visitError{400, "invalid_recurrence", "반복 예약은 2~52회, 전체 방문자 일정은 최대 500건까지 가능합니다"}
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	consent := consentContextFrom(r, actor.ID, consentSource(source))
	policy := map[string]string{"approvalEnabled": approval}
	policyJSON, _ := json.Marshal(policy)
	seriesID := ""
	if occurrences > 1 {
		seriesID = newID()
	}
	type createdVisit struct {
		ID        string `json:"id"`
		RequestNo string `json:"requestNo"`
		Status    string `json:"status"`
	}
	created := make([]createdVisit, 0, occurrences)
	passURLs := []string{}
	for occurrence := 0; occurrence < occurrences; occurrence++ {
		scheduled := in
		scheduled.StartAt = in.StartAt.AddDate(0, 0, occurrence*7)
		scheduled.EndAt = in.EndAt.AddDate(0, 0, occurrence*7)
		visitID := newID()
		suffix, randomErr := requestNoSuffix()
		if randomErr != nil {
			return nil, randomErr
		}
		requestNo := fmt.Sprintf("VF-%s-%s", scheduled.StartAt.Format("060102"), suffix)
		recurrenceValue := map[string]any{}
		if occurrences > 1 {
			recurrenceValue = map[string]any{"frequency": "weekly", "occurrences": occurrences, "index": occurrence + 1, "seriesId": seriesID}
		}
		recurrenceJSON, _ := json.Marshal(recurrenceValue)
		persistedStatus := status
		persistedParticipantStatus := participantStatus
		if autoCheckIn {
			persistedStatus, persistedParticipantStatus = "CHECKED_IN", "CHECKED_IN"
		}
		_, err = tx.Exec(ctx, `INSERT INTO visits(id,request_no,host_user_id,department_id,site_id,lobby_id,start_at,end_at,purpose,place_detail,notes,status,source,recurrence,policy_snapshot,visit_type_id,checklist) VALUES($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14,$15,NULLIF($16,''),$17)`, visitID, requestNo, hostID, scheduled.DepartmentID, scheduled.SiteID, scheduled.LobbyID, scheduled.StartAt, scheduled.EndAt, scheduled.Purpose, scheduled.PlaceDetail, scheduled.Notes, persistedStatus, source, recurrenceJSON, policyJSON, scheduled.VisitTypeID, checklistJSON)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO visit_events(visit_id,event_type,actor_user_id,method,details) VALUES($1,'REQUESTED',NULLIF($2,''),$3,$4)`, visitID, actor.ID, source, policyJSON)
		if err != nil {
			return nil, err
		}
		for visitorIndex, input := range scheduled.Visitors {
			visitorID, upsertErr := s.upsertVisitor(ctx, tx, input)
			if upsertErr != nil {
				return nil, upsertErr
			}
			vvID := newID()
			equipment, _ := json.Marshal(input.Equipment)
			_, err = tx.Exec(ctx, `INSERT INTO visitor_visits(id,visit_id,visitor_id,is_primary,equipment,status,checked_in_at) VALUES($1,$2,$3,$4,$5,$6,CASE WHEN $7 THEN now() ELSE NULL END)`, vvID, visitID, visitorID, visitorIndex == 0, equipment, persistedParticipantStatus, autoCheckIn)
			if err != nil {
				return nil, err
			}
			participantConsent := consent
			participantConsent.Locale = normalizeLocale(input.Locale)
			if err = s.recordConsentTx(ctx, tx, visitorID, visitID, vvID, participantConsent); err != nil {
				return nil, err
			}
			if status == "PENDING_APPROVAL" && visitorIndex == 0 {
				if err = s.queueApproverMailTx(ctx, tx, visitID, vvID, time.Now()); err != nil {
					return nil, err
				}
			}
			if status == "SCHEDULED" {
				raw, _, issueErr := s.issueQRTx(ctx, tx, vvID, scheduled.StartAt, scheduled.EndAt, "")
				if issueErr != nil {
					return nil, issueErr
				}
				if autoCheckIn {
					_, err = tx.Exec(ctx, `UPDATE qr_tokens SET used_at=now() WHERE visitor_visit_id=$1 AND revoked_at IS NULL`, vvID)
					if err != nil {
						return nil, err
					}
				}
				passURL := s.publicBaseURL(ctx, r) + "/q/" + raw
				if occurrence == 0 {
					passURLs = append(passURLs, passURL)
				}
				eventAt := time.Now()
				if queueErr := s.queueNotificationEventTx(ctx, tx, visitID, vvID, "visit_confirmed", eventAt); queueErr != nil {
					return nil, queueErr
				}
				if queueErr := s.queueNotificationEventTx(ctx, tx, visitID, vvID, "visit_start", eventAt); queueErr != nil {
					return nil, queueErr
				}
			}
			if autoCheckIn {
				_, err = tx.Exec(ctx, `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,actor_user_id,lobby_id,method) VALUES($1,$2,'CHECKED_IN',NULLIF($3,''),NULLIF($4,''),'walk_in')`, visitID, vvID, actor.ID, scheduled.LobbyID)
				if err != nil {
					return nil, err
				}
				if queueErr := s.queueNotificationEventTx(ctx, tx, visitID, vvID, "checked_in", time.Now()); queueErr != nil {
					return nil, queueErr
				}
			}
		}
		created = append(created, createdVisit{ID: visitID, RequestNo: requestNo, Status: persistedStatus})
	}
	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range created {
		s.audit(ctx, actor.ID, "visit.create", "visit", item.ID, requestRemote(r), map[string]any{"requestNo": item.RequestNo, "visitorCount": len(in.Visitors), "source": source, "seriesId": seriesID})
	}
	s.publishLobbyEvent("visit.updated")
	first := created[0]
	result := map[string]any{"id": first.ID, "requestNo": first.RequestNo, "status": first.Status, "visitorCount": len(in.Visitors), "occurrenceCount": len(created)}
	if len(created) > 1 {
		result["seriesId"] = seriesID
		result["occurrences"] = created
	}
	if len(passURLs) > 0 {
		result["passUrls"] = passURLs
	}
	if autoCheckIn {
		result["walkIn"] = true
	}
	return result, nil
}

// lobbyBelongsToSite rejects a lobby from another site; the lobby drives the
// pass, the scanner default and every lobby-scoped list.
func (s *Server) lobbyBelongsToSite(ctx context.Context, lobbyID, siteID string) error {
	if strings.TrimSpace(lobbyID) == "" {
		return nil
	}
	var matches bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lobbies WHERE id=$1 AND site_id=$2 AND active)`, lobbyID, siteID).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return visitError{400, "lobby_site_mismatch", "선택한 로비가 해당 사업장에 속하지 않습니다"}
	}
	return nil
}

// actsForHost reports whether u is the host or the host's active delegate.
func (s *Server) actsForHost(ctx context.Context, u User, hostID string) bool {
	if u.ID == "" {
		return false
	}
	if u.ID == hostID {
		return true
	}
	var delegated bool
	_ = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND delegate_user_id=$2 AND delegate_until>now())`, hostID, u.ID).Scan(&delegated)
	return delegated
}

func requestRemote(r *http.Request) string {
	if r == nil {
		return "mcp"
	}
	return clientIP(r)
}

func (s *Server) upsertVisitor(ctx context.Context, tx pgx.Tx, in VisitorInput) (string, error) {
	phone := normalizePhone(in.Phone)
	hash := s.keys.Digest("phone:" + phone)
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM visitors WHERE phone_hash=$1 AND erased_at IS NULL ORDER BY updated_at DESC LIMIT 1`, hash).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	nameEnc, err := s.keys.Encrypt(strings.TrimSpace(in.Name))
	if err != nil {
		return "", err
	}
	phoneEnc, err := s.keys.Encrypt(phone)
	if err != nil {
		return "", err
	}
	emailEnc, err := s.encryptOptional(in.Email)
	if err != nil {
		return "", err
	}
	vehicleEnc, err := s.encryptOptional(in.Vehicle)
	if err != nil {
		return "", err
	}
	locale := normalizeLocale(in.Locale)
	if id == "" {
		id = newID()
		_, err = tx.Exec(ctx, `INSERT INTO visitors(id,name_encrypted,name_hash,phone_encrypted,phone_hash,email_encrypted,company,title,vehicle_encrypted,locale,consented_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,now())`, id, nameEnc, s.keys.Digest("name:"+strings.ToLower(strings.TrimSpace(in.Name))), phoneEnc, hash, emailEnc, strings.TrimSpace(in.Company), strings.TrimSpace(in.Title), vehicleEnc, locale)
	} else {
		// An empty locale keeps whatever the visitor chose on a previous visit.
		_, err = tx.Exec(ctx, `UPDATE visitors SET name_encrypted=$2,name_hash=$3,phone_encrypted=$4,email_encrypted=NULLIF($5,''),company=NULLIF($6,''),title=NULLIF($7,''),vehicle_encrypted=NULLIF($8,''),locale=COALESCE(NULLIF($9,''),locale),consented_at=now(),masked_at=NULL,updated_at=now() WHERE id=$1`, id, nameEnc, s.keys.Digest("name:"+strings.ToLower(strings.TrimSpace(in.Name))), phoneEnc, emailEnc, strings.TrimSpace(in.Company), strings.TrimSpace(in.Title), vehicleEnc, locale)
	}
	return id, err
}

func (s *Server) publicBaseURL(ctx context.Context, r *http.Request) string {
	base, _ := s.getSetting(ctx, "general.base_url")
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" && r != nil {
		base = requestBaseURL(r)
	}
	return base
}

func (s *Server) issueQRTx(ctx context.Context, tx pgx.Tx, participantID string, startAt, endAt time.Time, rotatedFrom string) (string, string, error) {
	random, err := platform.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	raw := "vfq_" + random
	fileSeq, err := platform.RandomToken(18)
	if err != nil {
		return "", "", err
	}
	encrypted, err := s.keys.Encrypt(raw)
	if err != nil {
		return "", "", err
	}
	version := 1
	if rotatedFrom != "" {
		_ = tx.QueryRow(ctx, `SELECT version+1 FROM qr_tokens WHERE id=$1`, rotatedFrom).Scan(&version)
	}
	earlyMinutes, _ := strconv.Atoi(settingOr(s, ctx, "visit.early_checkin_minutes", "60"))
	lateMinutes, _ := strconv.Atoi(settingOr(s, ctx, "visit.late_grace_minutes", "120"))
	if earlyMinutes < 0 || earlyMinutes > 1440 {
		earlyMinutes = 60
	}
	if lateMinutes < 0 || lateMinutes > 1440 {
		lateMinutes = 120
	}
	validFrom := startAt.Add(-time.Duration(earlyMinutes) * time.Minute)
	validUntil := endAt.Add(time.Duration(lateMinutes) * time.Minute)
	_, err = tx.Exec(ctx, `INSERT INTO qr_tokens(id,visitor_visit_id,token_hash,token_encrypted,prefix,version,valid_from,valid_until,rotated_from,qrcode_file_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10)`, newID(), participantID, s.keys.Digest("qr:"+raw), encrypted, raw[:12], version, validFrom, validUntil, rotatedFrom, fileSeq)
	return raw, fileSeq, err
}

func (s *Server) queueVisitorNotificationTx(ctx context.Context, tx pgx.Tx, visitID, phone string, visitor VisitorInput, visit VisitInput, passURL string) error {
	tmpl, _ := s.getSetting(ctx, "notification.visitor_template")
	company, _ := s.getSetting(ctx, "general.company_name")
	place := visit.PlaceDetail
	if place == "" {
		_ = tx.QueryRow(ctx, `SELECT name FROM sites WHERE id=$1`, visit.SiteID).Scan(&place)
	}
	body := renderTemplate(tmpl, map[string]string{"company": company, "visitor": visitor.Name, "start": visit.StartAt.Local().Format("2006-01-02 15:04"), "place": place, "passUrl": passURL})
	return s.queueNotificationTx(ctx, tx, visitID, phone, "sms", "visitor_pass", body)
}

func renderTemplate(tmpl string, values map[string]string) string {
	for key, value := range values {
		tmpl = strings.ReplaceAll(tmpl, "{{"+key+"}}", value)
	}
	return tmpl
}

func (s *Server) queueNotificationTx(ctx context.Context, tx pgx.Tx, visitID, recipient, channel, key, body string) error {
	if strings.TrimSpace(recipient) == "" {
		return nil
	}
	recipientEnc, err := s.keys.Encrypt(strings.TrimSpace(recipient))
	if err != nil {
		return err
	}
	bodyEnc, err := s.keys.Encrypt(body)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(id,visit_id,recipient_encrypted,channel,template_key,body_encrypted) VALUES($1,$2,$3,$4,$5,$6)`, newID(), visitID, recipientEnc, channel, key, bodyEnc)
	return err
}

func (s *Server) getVisit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "visitID")
	u, _ := userFrom(r)
	items, _, err := s.queryVisits(r.Context(), u, visitQuery{ID: id, Limit: 2})
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	var summary *VisitSummary
	for i := range items {
		if items[i].ID == id || items[i].RequestNo == id {
			summary = &items[i]
			break
		}
	}
	if summary == nil {
		writeError(w, http.StatusNotFound, "not_found", "방문 요청을 찾을 수 없습니다")
		return
	}
	participants, err := s.visitParticipants(r.Context(), id, true)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	extras, err := s.visitDetailExtras(r.Context(), id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit.view", "visit", id, clientIP(r), map[string]any{"visitorCount": len(participants)})
	writeJSON(w, http.StatusOK, map[string]any{"visit": summary, "visitors": participants, "detail": extras})
}

func (s *Server) visitParticipants(ctx context.Context, visitID string, includePass bool) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `SELECT vv.id,p.id,p.name_encrypted,p.phone_encrypted,COALESCE(p.email_encrypted,''),COALESCE(p.company,''),COALESCE(p.title,''),COALESCE(p.vehicle_encrypted,''),p.masked_at,p.locale,vv.equipment,vv.status,vv.badge_no,vv.checked_in_at,vv.checked_out_at,
		COALESCE(q.token_encrypted,''),q.version,COALESCE(q.qrcode_file_seq,''),ri.expires_at,COALESCE(c.source,'')
		FROM visitor_visits vv JOIN visitors p ON p.id=vv.visitor_id
		LEFT JOIN LATERAL (SELECT token_encrypted,version,qrcode_file_seq FROM qr_tokens WHERE visitor_visit_id=vv.id AND revoked_at IS NULL ORDER BY issued_at DESC LIMIT 1) q ON true
		LEFT JOIN LATERAL (SELECT expires_at FROM registration_invitations WHERE visitor_visit_id=vv.id AND completed_at IS NULL AND revoked_at IS NULL AND expires_at>now() LIMIT 1) ri ON true
		LEFT JOIN LATERAL (SELECT source FROM consent_records WHERE visitor_visit_id=vv.id ORDER BY consented_at DESC LIMIT 1) c ON true
		WHERE vv.visit_id=$1 ORDER BY vv.is_primary DESC,vv.created_at`, visitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var vvID, visitorID, nameEnc, phoneEnc, emailEnc, company, title, vehicleEnc, locale, status, tokenEnc, fileSeq, consentSourceValue string
		var equipment []byte
		var maskedAt *time.Time
		var badge *string
		var checkedIn, checkedOut, invitationExpiresAt *time.Time
		var version *int
		if err := rows.Scan(&vvID, &visitorID, &nameEnc, &phoneEnc, &emailEnc, &company, &title, &vehicleEnc, &maskedAt, &locale, &equipment, &status, &badge, &checkedIn, &checkedOut, &tokenEnc, &version, &fileSeq, &invitationExpiresAt, &consentSourceValue); err != nil {
			return nil, err
		}
		var equipmentValue any = []string{}
		_ = json.Unmarshal(equipment, &equipmentValue)
		name, email, vehicle := s.decryptOptional(nameEnc), s.decryptOptional(emailEnc), s.decryptOptional(vehicleEnc)
		if maskedAt != nil {
			name, email, vehicle = maskName(name), "", ""
		}
		item := map[string]any{"id": vvID, "visitorId": visitorID, "name": name, "phone": maskPhone(s.decryptOptional(phoneEnc)), "email": email, "company": company, "title": title, "vehicle": vehicle, "equipment": equipmentValue, "status": status, "badgeNo": badge, "checkedInAt": checkedIn, "checkedOutAt": checkedOut, "qrVersion": version, "maskedAt": maskedAt, "locale": locale, "consentSource": consentSourceValue, "registrationInviteExpiresAt": invitationExpiresAt}
		if includePass && tokenEnc != "" {
			item["passPath"] = "/q/" + s.decryptOptional(tokenEnc)
			item["qrcodeImagePath"] = "/img/visitor/" + fileSeq + ".jpg"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) updateVisit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StartAt     time.Time `json:"startAt"`
		EndAt       time.Time `json:"endAt"`
		Purpose     string    `json:"purpose"`
		PlaceDetail string    `json:"placeDetail"`
		LobbyID     string    `json:"lobbyId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.EndAt.After(in.StartAt) || strings.TrimSpace(in.Purpose) == "" {
		writeError(w, 400, "invalid_visit", "일정과 방문 목적을 확인하세요")
		return
	}
	if in.EndAt.Sub(in.StartAt) > 31*24*time.Hour {
		writeError(w, 400, "schedule_too_long", "한 방문 일정은 31일을 초과할 수 없습니다")
		return
	}
	u, _ := userFrom(r)
	id := chi.URLParam(r, "visitID")
	var oldStart, oldEnd time.Time
	var oldPurpose, oldPlace, oldLobby, siteID string
	if err := s.db.QueryRow(r.Context(), `SELECT start_at,end_at,purpose,COALESCE(place_detail,''),COALESCE(lobby_id,''),site_id FROM visits
		WHERE id=$1 AND (host_user_id=$2 OR $3 OR EXISTS(SELECT 1 FROM users hu WHERE hu.id=visits.host_user_id AND hu.delegate_user_id=$2 AND hu.delegate_until>now()))
		AND status IN ('PENDING_APPROVAL','SCHEDULED','APPROVED')`, id, u.ID, u.IsAdmin()).Scan(&oldStart, &oldEnd, &oldPurpose, &oldPlace, &oldLobby, &siteID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err := s.lobbyBelongsToSite(r.Context(), in.LobbyID, siteID); err != nil {
		writeVisitError(w, err)
		return
	}
	earlyMinutes, _ := strconv.Atoi(settingOr(s, r.Context(), "visit.early_checkin_minutes", "60"))
	lateMinutes, _ := strconv.Atoi(settingOr(s, r.Context(), "visit.late_grace_minutes", "120"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `UPDATE visits SET start_at=$2,end_at=$3,purpose=$4,place_detail=NULLIF($5,''),lobby_id=NULLIF($6,''),updated_at=now() WHERE id=$1`, id, in.StartAt, in.EndAt, strings.TrimSpace(in.Purpose), in.PlaceDetail, in.LobbyID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE qr_tokens SET valid_from=$2-($3::int*interval '1 minute'),valid_until=$4+($5::int*interval '1 minute') WHERE visitor_visit_id IN (SELECT id FROM visitor_visits WHERE visit_id=$1) AND revoked_at IS NULL`, id, in.StartAt, earlyMinutes, in.EndAt, lateMinutes)
	}
	if err == nil {
		err = s.refreshVisitStartNotificationsTx(r.Context(), tx, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit.update", "visit", id, clientIP(r), map[string]any{
		"before": map[string]any{"startAt": oldStart, "endAt": oldEnd, "purpose": oldPurpose, "placeDetail": oldPlace, "lobbyId": oldLobby},
		"after":  in,
	})
	s.publishLobbyEvent("visit.updated")
	w.WriteHeader(http.StatusNoContent)
}

// cancelVisitTx cancels a visit and everything attached to it: participant
// status, active QR tokens, pending notifications, the timeline event and the
// visit_cancelled rules. It reports false when the visit was not cancellable by
// this actor so REST and MCP return the same 409 semantics.
func (s *Server) cancelVisitTx(ctx context.Context, tx pgx.Tx, id string, u User) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE visits SET status='CANCELLED',cancelled_at=now(),updated_at=now()
		WHERE id=$1 AND (host_user_id=$2 OR $3 OR EXISTS(SELECT 1 FROM users hu WHERE hu.id=visits.host_user_id AND hu.delegate_user_id=$2 AND hu.delegate_until>now()))
		AND status NOT IN ('CHECKED_OUT','CANCELLED','REJECTED')`, id, u.ID, u.IsAdmin() || u.Role == RoleSecurity)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE visitor_visits SET status='CANCELLED' WHERE visit_id=$1 AND status NOT IN ('CHECKED_OUT','CANCELLED')`, id); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE qr_tokens SET revoked_at=now() WHERE visitor_visit_id IN (SELECT id FROM visitor_visits WHERE visit_id=$1) AND revoked_at IS NULL`, id); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO visit_events(visit_id,event_type,actor_user_id) VALUES($1,'CANCELLED',NULLIF($2,''))`, id, u.ID); err != nil {
		return false, err
	}
	if err = s.cancelPendingVisitNotificationsTx(ctx, tx, id); err != nil {
		return false, err
	}
	participantIDs := []string{}
	rows, err := tx.Query(ctx, `SELECT id FROM visitor_visits WHERE visit_id=$1 ORDER BY created_at`, id)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var participantID string
		if err := rows.Scan(&participantID); err != nil {
			rows.Close()
			return false, err
		}
		participantIDs = append(participantIDs, participantID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, participantID := range participantIDs {
		if err := s.queueNotificationEventTx(ctx, tx, id, participantID, "visit_cancelled", time.Now()); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Server) cancelVisit(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "visitID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	cancelled, err := s.cancelVisitTx(r.Context(), tx, id, u)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !cancelled {
		writeError(w, 409, "cannot_cancel", "취소할 수 없는 방문 상태이거나 권한이 없습니다")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit.cancel", "visit", id, clientIP(r), nil)
	s.publishLobbyEvent("visit.cancelled")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveVisit(w http.ResponseWriter, r *http.Request) {
	s.approvalAction(w, r, true)
}

func (s *Server) rejectVisit(w http.ResponseWriter, r *http.Request) {
	s.approvalAction(w, r, false)
}

func (s *Server) approvalAction(w http.ResponseWriter, r *http.Request, approve bool) {
	u, _ := userFrom(r)
	if !u.CanApprove() {
		writeError(w, 403, "approver_required", "승인 권한이 필요합니다")
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "visitID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	status := "REJECTED"
	participantStatus := "REJECTED"
	if approve {
		status, participantStatus = "SCHEDULED", "SCHEDULED"
	}
	departmentID := ""
	if u.DepartmentID != nil {
		departmentID = *u.DepartmentID
	}
	var startAt, endAt time.Time
	tag, err := tx.Exec(r.Context(), `UPDATE visits SET status=$2,approval_reason=NULLIF($3,''),approved_by=$4,approved_at=now(),updated_at=now()
		WHERE id=$1 AND status='PENDING_APPROVAL' AND (
			$5 IN ('security','admin','super_admin')
			OR ($5='dept_manager' AND department_id=NULLIF($6,''))
			OR EXISTS(SELECT 1 FROM users m WHERE m.delegate_user_id=$4 AND m.delegate_until>now() AND m.active AND m.role='dept_manager' AND m.department_id=visits.department_id)
		)`, id, status, in.Reason, u.ID, u.Role, departmentID)
	if err == nil && tag.RowsAffected() > 0 {
		err = tx.QueryRow(r.Context(), `SELECT start_at,end_at FROM visits WHERE id=$1`, id).Scan(&startAt, &endAt)
	}
	if err == nil && tag.RowsAffected() > 0 {
		_, err = tx.Exec(r.Context(), `UPDATE visitor_visits SET status=$2 WHERE visit_id=$1`, id, participantStatus)
	}
	if err == nil && approve {
		participants := []string{}
		rows, queryErr := tx.Query(r.Context(), `SELECT id FROM visitor_visits WHERE visit_id=$1 ORDER BY created_at`, id)
		if queryErr != nil {
			err = queryErr
		} else if err == nil {
			for rows.Next() {
				var participantID string
				if scanErr := rows.Scan(&participantID); scanErr != nil {
					err = scanErr
					break
				}
				participants = append(participants, participantID)
			}
			if rows.Err() != nil {
				err = rows.Err()
			}
			rows.Close()
			for _, participantID := range participants {
				_, _, issueErr := s.issueQRTx(r.Context(), tx, participantID, startAt, endAt, "")
				if issueErr != nil {
					err = issueErr
					break
				}
				eventAt := time.Now()
				if queueErr := s.queueNotificationEventTx(r.Context(), tx, id, participantID, "visit_confirmed", eventAt); queueErr != nil {
					err = queueErr
					break
				}
				if queueErr := s.queueNotificationEventTx(r.Context(), tx, id, participantID, "visit_start", eventAt); queueErr != nil {
					err = queueErr
					break
				}
			}
		}
	}
	if err == nil && tag.RowsAffected() > 0 {
		_, err = tx.Exec(r.Context(), `INSERT INTO visit_events(visit_id,event_type,actor_user_id,details) VALUES($1,$2,NULLIF($3,''),$4)`, id, status, u.ID, map[string]string{"reason": in.Reason})
	}
	if err == nil && tag.RowsAffected() > 0 && !approve {
		// Rejection has no QR to hand out, but the host must learn the outcome.
		if err = s.cancelPendingVisitNotificationsTx(r.Context(), tx, id); err == nil {
			var primary string
			if scanErr := tx.QueryRow(r.Context(), `SELECT id FROM visitor_visits WHERE visit_id=$1 ORDER BY is_primary DESC,created_at LIMIT 1`, id).Scan(&primary); scanErr == nil {
				err = s.queueNotificationEventTx(r.Context(), tx, id, primary, "visit_rejected", time.Now())
			}
		}
	}
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 409, "approval_conflict", "승인 대기 상태가 아니거나 처리할 수 없습니다")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit."+strings.ToLower(status), "visit", id, clientIP(r), map[string]string{"reason": in.Reason})
	s.publishLobbyEvent("visit.approval")
	w.WriteHeader(http.StatusNoContent)
}

func qrParticipantStatusTerminal(status string) bool {
	switch status {
	case "CHECKED_OUT", "CANCELLED", "REJECTED", "NO_SHOW":
		return true
	default:
		return false
	}
}

func qrAvailableForNotification(status string, validUntil, revokedAt *time.Time, now time.Time) bool {
	return !qrParticipantStatusTerminal(status) && validUntil != nil && revokedAt == nil && !now.After(*validUntil)
}

func (s *Server) reissueQR(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	participantID := chi.URLParam(r, "visitorVisitID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var visitID, participantStatus string
	err = tx.QueryRow(r.Context(), `SELECT visit_id,status FROM visitor_visits WHERE id=$1 FOR UPDATE`, participantID).Scan(&visitID, &participantStatus)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	var hostID, visitStatus string
	var startAt, endAt time.Time
	err = tx.QueryRow(r.Context(), `SELECT host_user_id,start_at,end_at,status FROM visits WHERE id=$1`, visitID).Scan(&hostID, &startAt, &endAt, &visitStatus)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !s.actsForHost(r.Context(), u, hostID) && !u.CanManageLobby() && !u.IsAdmin() {
		writeError(w, 403, "reissue_forbidden", "QR을 재발급할 수 없습니다")
		return
	}
	if qrParticipantStatusTerminal(participantStatus) {
		writeError(w, http.StatusConflict, "participant_not_reissuable", "종료·취소·반려 또는 미방문 처리된 방문자는 QR을 재발급할 수 없습니다")
		return
	}
	if visitStatus != "SCHEDULED" && visitStatus != "APPROVED" {
		writeError(w, http.StatusConflict, "visit_not_reissuable", "QR을 재발급할 수 없는 방문 상태입니다")
		return
	}
	var oldID string
	err = tx.QueryRow(r.Context(), `SELECT id FROM qr_tokens WHERE visitor_visit_id=$1 AND revoked_at IS NULL ORDER BY issued_at DESC,version DESC,id DESC LIMIT 1 FOR UPDATE`, participantID).Scan(&oldID)
	if errors.Is(err, pgx.ErrNoRows) {
		oldID, err = "", nil
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE qr_tokens SET revoked_at=now() WHERE visitor_visit_id=$1 AND revoked_at IS NULL`, participantID)
	}
	var raw, fileSeq string
	if err == nil {
		raw, fileSeq, err = s.issueQRTx(r.Context(), tx, participantID, startAt, endAt, oldID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE notifications SET status='cancelled',error=NULL,
			attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,claimed_at=NULL,claim_token=NULL
			WHERE visitor_visit_id=$1 AND status IN ('queued','failed','sending') AND (
				(rule_id IS NULL AND template_key='visitor_pass') OR rule_id IN (SELECT id FROM notification_rules WHERE event='visit_confirmed')
			)`, participantID)
	}
	if err == nil {
		err = s.queueNotificationEventTx(r.Context(), tx, visitID, participantID, "visit_confirmed", time.Now())
	}
	if err == nil {
		err = s.refreshVisitStartNotificationsTx(r.Context(), tx, visitID, participantID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "qr.reissue", "visitor_visit", participantID, clientIP(r), map[string]string{"visitId": visitID})
	baseURL := s.publicBaseURL(r.Context(), r)
	writeJSON(w, 201, map[string]string{
		"passUrl": strings.TrimRight(baseURL, "/") + "/q/" + raw, "qrcodeFileSeq": fileSeq,
		"qrcodeImageUrl": strings.TrimRight(baseURL, "/") + "/img/visitor/" + fileSeq + ".jpg",
	})
}

func (s *Server) resendVisitNotification(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "visitID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var hostID, status string
	if err = tx.QueryRow(r.Context(), `SELECT host_user_id,status FROM visits WHERE id=$1 FOR UPDATE`, id).Scan(&hostID, &status); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if !s.actsForHost(r.Context(), u, hostID) && !u.CanManageLobby() && !u.IsAdmin() {
		writeError(w, 403, "forbidden", "재발송 권한이 없습니다")
		return
	}
	if status != "APPROVED" && status != "SCHEDULED" && status != "ARRIVED" && status != "CHECKED_IN" {
		writeError(w, http.StatusConflict, "visit_not_notifiable", "진행 중인 방문만 방문 안내를 재발송할 수 있습니다")
		return
	}
	participantIDs := []string{}
	rows, err := tx.Query(r.Context(), `SELECT vv.id,vv.status,q.valid_until,q.revoked_at
		FROM visitor_visits vv
		LEFT JOIN LATERAL (
			SELECT valid_until,revoked_at FROM qr_tokens
			WHERE visitor_visit_id=vv.id
			ORDER BY issued_at DESC,version DESC,id DESC LIMIT 1
		) q ON true
		WHERE vv.visit_id=$1 ORDER BY vv.created_at`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	for rows.Next() {
		var participantID, participantStatus string
		var validUntil, revokedAt *time.Time
		if err = rows.Scan(&participantID, &participantStatus, &validUntil, &revokedAt); err != nil {
			rows.Close()
			notFoundOrServer(w, err)
			return
		}
		if qrAvailableForNotification(participantStatus, validUntil, revokedAt, time.Now()) {
			participantIDs = append(participantIDs, participantID)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		notFoundOrServer(w, err)
		return
	}
	rows.Close()
	if len(participantIDs) == 0 {
		writeError(w, http.StatusConflict, "no_valid_qr", "재발송할 수 있는 유효한 방문자 QR이 없습니다")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE notifications SET status='cancelled',error=NULL,
		attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,claimed_at=NULL,claim_token=NULL
		WHERE visit_id=$1 AND status IN ('queued','failed','sending') AND (
			(rule_id IS NULL AND template_key='visitor_pass') OR rule_id IN (SELECT id FROM notification_rules WHERE event='visit_confirmed')
		)`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	queued := 0
	for _, participantID := range participantIDs {
		if err = s.queueNotificationEventCountTx(r.Context(), tx, id, participantID, "visit_confirmed", time.Now(), &queued); err != nil {
			notFoundOrServer(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "notification.resend", "visit", id, clientIP(r), map[string]int{"count": queued})
	writeJSON(w, 200, map[string]int{"queued": queued})
}

func (s *Server) createWalkIn(w http.ResponseWriter, r *http.Request) {
	var in VisitInput
	if !decodeJSON(w, r, &in) {
		return
	}
	u, _ := userFrom(r)
	result, err := s.createVisitRecord(r.Context(), r, u, in, "lobby", true)
	if err != nil {
		writeVisitError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

type qrRecord struct {
	TokenID, ParticipantID, VisitID, Status, ParticipantStatus, NameEnc, Company, HostName, Department, SiteID, SiteName, LobbyName, Purpose string
	Locale                                                                                                                                   string
	StartAt, EndAt, ValidFrom, ValidUntil                                                                                                    time.Time
	UsedAt, RevokedAt                                                                                                                        *time.Time
	Version                                                                                                                                  int
}

func (s *Server) lookupQR(ctx context.Context, raw string) (qrRecord, error) {
	var q qrRecord
	err := s.db.QueryRow(ctx, `SELECT qt.id,vv.id,v.id,v.status,vv.status,p.name_encrypted,COALESCE(p.company,''),h.display_name,COALESCE(o.name,''),si.id,si.name,COALESCE(l.name,''),v.purpose,p.locale,v.start_at,v.end_at,qt.valid_from,qt.valid_until,qt.used_at,qt.revoked_at,qt.version FROM qr_tokens qt JOIN visitor_visits vv ON vv.id=qt.visitor_visit_id JOIN visitors p ON p.id=vv.visitor_id JOIN visits v ON v.id=vv.visit_id JOIN users h ON h.id=v.host_user_id JOIN sites si ON si.id=v.site_id LEFT JOIN lobbies l ON l.id=v.lobby_id LEFT JOIN organizations o ON o.id=v.department_id WHERE qt.token_hash=$1`, s.keys.Digest("qr:"+raw)).Scan(&q.TokenID, &q.ParticipantID, &q.VisitID, &q.Status, &q.ParticipantStatus, &q.NameEnc, &q.Company, &q.HostName, &q.Department, &q.SiteID, &q.SiteName, &q.LobbyName, &q.Purpose, &q.Locale, &q.StartAt, &q.EndAt, &q.ValidFrom, &q.ValidUntil, &q.UsedAt, &q.RevokedAt, &q.Version)
	return q, err
}

func parseQRValue(value string) (raw, ts, sig string) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 && parts[len(parts)-2] == "q" {
			value = parts[len(parts)-1]
			ts, sig = parsed.Query().Get("ts"), parsed.Query().Get("sig")
		}
	}
	return value, ts, sig
}

func (s *Server) validateDynamicQR(ctx context.Context, raw, ts, sig string) bool {
	seconds, _ := strconv.Atoi(settingOr(s, ctx, "visit.dynamic_qr_seconds", "0"))
	return s.validateQRSignature(raw, ts, sig, seconds, time.Now())
}

func (s *Server) validateQRSignature(raw, ts, sig string, seconds int, now time.Time) bool {
	if seconds <= 0 {
		return true
	}
	if ts == "static" {
		expected := hex.EncodeToString(s.keys.Digest("qr-static:" + raw))[:24]
		return sig != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1
	}
	window, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return false
	}
	current := now.Unix() / int64(seconds)
	if window < current-1 || window > current+1 {
		return false
	}
	expected := hex.EncodeToString(s.keys.Digest("dynamic:" + raw + ":" + ts))[:24]
	return subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1
}

func settingOr(s *Server, ctx context.Context, key, fallback string) string {
	v, err := s.getSetting(ctx, key)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

func (s *Server) qrURL(ctx context.Context, base, raw string) string {
	value := strings.TrimRight(base, "/") + "/q/" + raw
	seconds, _ := strconv.Atoi(settingOr(s, ctx, "visit.dynamic_qr_seconds", "0"))
	if seconds > 0 {
		ts := strconv.FormatInt(time.Now().Unix()/int64(seconds), 10)
		sig := hex.EncodeToString(s.keys.Digest("dynamic:" + raw + ":" + ts))[:24]
		value += "?ts=" + ts + "&sig=" + sig
	}
	return value
}

func (s *Server) staticQRURL(base, raw string) string {
	sig := hex.EncodeToString(s.keys.Digest("qr-static:" + raw))[:24]
	return strings.TrimRight(base, "/") + "/q/" + raw + "?ts=static&sig=" + sig
}

func (s *Server) publicPass(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "token")
	q, err := s.lookupQR(r.Context(), raw)
	if err != nil || q.RevokedAt != nil {
		writeError(w, 404, "pass_not_found", "유효한 모바일 방문증을 찾을 수 없습니다")
		return
	}
	status := q.ParticipantStatus
	if time.Now().After(q.ValidUntil) && status == "SCHEDULED" {
		status = "EXPIRED"
	}
	locale := s.negotiateLocale(r.Context(), r.URL.Query().Get("lang"), q.Locale, r.Header.Get("Accept-Language"))
	writeJSON(w, 200, map[string]any{"visitor": maskName(s.decryptOptional(q.NameEnc)), "company": q.Company, "host": maskName(q.HostName), "department": q.Department, "site": q.SiteName, "lobby": q.LobbyName, "purpose": q.Purpose, "startAt": q.StartAt, "endAt": q.EndAt, "status": status, "version": q.Version, "locale": locale, "supportedLocales": s.supportedLocales(r.Context()), "qrImageUrl": fmt.Sprintf("/api/v1/public/passes/%s/qr.png?v=%d&t=%d", raw, q.Version, time.Now().Unix()/30)})
}

func (s *Server) publicPassQR(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "token")
	q, err := s.lookupQR(r.Context(), raw)
	if err != nil || q.RevokedAt != nil || time.Now().After(q.ValidUntil) || q.Status == "CANCELLED" || q.Status == "REJECTED" {
		writeError(w, 404, "pass_not_found", "유효한 모바일 방문증을 찾을 수 없습니다")
		return
	}
	png, err := qrcode.Encode(s.qrURL(r.Context(), s.publicBaseURL(r.Context(), r), raw), qrcode.Medium, 560)
	if err != nil {
		writeError(w, 500, "qr_failed", "QR 이미지를 만들지 못했습니다")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func validQRCodeFileSeq(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func encodeQRJPEG(value string) ([]byte, error) {
	code, err := qrcode.New(value, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, code.Image(560), &jpeg.Options{Quality: 100}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (s *Server) publicVisitorQRJPEG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	fileSeq := chi.URLParam(r, "qrcode_file_seq")
	if !validQRCodeFileSeq(fileSeq) {
		http.NotFound(w, r)
		return
	}
	var tokenEncrypted, visitStatus, participantStatus string
	var validUntil time.Time
	var revokedAt *time.Time
	err := s.db.QueryRow(r.Context(), `SELECT qt.token_encrypted,v.status,vv.status,qt.valid_until,qt.revoked_at
		FROM qr_tokens qt
		JOIN visitor_visits vv ON vv.id=qt.visitor_visit_id
		JOIN visits v ON v.id=vv.visit_id
		WHERE qt.qrcode_file_seq=$1`, fileSeq).Scan(&tokenEncrypted, &visitStatus, &participantStatus, &validUntil, &revokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("public qr lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "qr_failed", "QR 이미지를 조회하지 못했습니다")
		return
	}
	if revokedAt != nil || time.Now().After(validUntil) || qrParticipantStatusTerminal(visitStatus) || qrParticipantStatusTerminal(participantStatus) {
		http.NotFound(w, r)
		return
	}
	raw, err := s.keys.Decrypt(tokenEncrypted)
	if err != nil {
		s.logger.Error("public qr token decrypt failed", "error", err, "qrcode_file_seq", fileSeq)
		writeError(w, http.StatusInternalServerError, "qr_failed", "QR 이미지를 만들지 못했습니다")
		return
	}
	image, err := encodeQRJPEG(s.staticQRURL(s.publicBaseURL(r.Context(), r), raw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "qr_failed", "QR 이미지를 만들지 못했습니다")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.jpg"`, fileSeq))
	w.Header().Set("Content-Length", strconv.Itoa(len(image)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(image)
	}
}

func (s *Server) verifyQR(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	raw, ts, sig := parseQRValue(in.Token)
	q, err := s.lookupQR(r.Context(), raw)
	u, _ := userFrom(r)
	result := "valid"
	status := http.StatusOK
	message := "체크인할 수 있습니다"
	if err != nil {
		result, status, message = "not_found", 404, "존재하지 않는 QR입니다"
	} else if !siteAllowed(u, q.SiteID) {
		result, status, message = "wrong_site", 403, "담당 사업장이 아닌 방문증입니다"
	} else if q.RevokedAt != nil || q.Status == "CANCELLED" || q.Status == "REJECTED" {
		result, status, message = "revoked", 409, "취소 또는 폐기된 방문증입니다"
	} else if !s.validateDynamicQR(r.Context(), raw, ts, sig) {
		result, status, message = "dynamic_expired", 409, "갱신된 모바일 방문증 QR을 다시 제시해 주세요"
	} else if time.Now().Before(q.ValidFrom) {
		result, status, message = "too_early", 409, "아직 사용할 수 없는 방문증입니다"
	} else if time.Now().After(q.ValidUntil) {
		result, status, message = "expired", 409, "유효기간이 지난 방문증입니다"
	} else if q.UsedAt != nil && settingOr(s, r.Context(), "visit.single_use_qr", "true") == "true" {
		result, status, message = "already_used", 409, "이미 체크인에 사용된 방문증입니다"
	} else if qrParticipantStatusTerminal(q.ParticipantStatus) {
		result, status, message = "participant_closed", 409, "이미 종료 처리된 방문자입니다"
	}
	details := map[string]any{"result": result}
	if q.VisitID != "" {
		details["visitId"] = q.VisitID
	}
	s.audit(r.Context(), u.ID, "qr.verify", "qr_token", q.TokenID, clientIP(r), details)
	if status != http.StatusOK {
		s.metrics.qrRejected.Add(1)
		writeError(w, status, result, message)
		return
	}
	s.metrics.qrVerified.Add(1)
	writeJSON(w, 200, map[string]any{"valid": true, "message": message, "token": raw, "visitorVisitId": q.ParticipantID, "visitId": q.VisitID, "visitor": s.decryptOptional(q.NameEnc), "company": q.Company, "host": q.HostName, "department": q.Department, "site": q.SiteName, "lobby": q.LobbyName, "purpose": q.Purpose, "startAt": q.StartAt, "endAt": q.EndAt})
}

func (s *Server) checkIn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token   string `json:"token"`
		LobbyID string `json:"lobbyId"`
		Method  string `json:"method"`
		BadgeNo string `json:"badgeNo"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	raw, ts, sig := parseQRValue(in.Token)
	if !s.validateDynamicQR(r.Context(), raw, ts, sig) {
		writeError(w, 409, "dynamic_expired", "갱신된 QR을 다시 스캔하세요")
		return
	}
	u, _ := userFrom(r)
	// An unattended kiosk knows which lobby it stands in; record that lobby
	// unless the request names one explicitly.
	if device, ok := kioskFrom(r); ok && in.LobbyID == "" {
		in.LobbyID = device.LobbyID
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var tokenID, participantID, visitID, visitStatus, participantStatus, hostID, visitorNameEnc, lobbyName, siteID string
	var validFrom, validUntil time.Time
	var usedAt, revokedAt *time.Time
	err = tx.QueryRow(r.Context(), `SELECT qt.id,vv.id,v.id,v.status,vv.status,v.host_user_id,p.name_encrypted,COALESCE(l.name,''),v.site_id,qt.valid_from,qt.valid_until,qt.used_at,qt.revoked_at FROM qr_tokens qt JOIN visitor_visits vv ON vv.id=qt.visitor_visit_id JOIN visitors p ON p.id=vv.visitor_id JOIN visits v ON v.id=vv.visit_id LEFT JOIN lobbies l ON l.id=COALESCE(NULLIF($2,''),v.lobby_id) WHERE qt.token_hash=$1 FOR UPDATE OF qt,vv,v,p`, s.keys.Digest("qr:"+raw), in.LobbyID).Scan(&tokenID, &participantID, &visitID, &visitStatus, &participantStatus, &hostID, &visitorNameEnc, &lobbyName, &siteID, &validFrom, &validUntil, &usedAt, &revokedAt)
	now := time.Now()
	if err != nil || !siteAllowed(u, siteID) || revokedAt != nil || now.Before(validFrom) || now.After(validUntil) || visitStatus == "CANCELLED" || visitStatus == "REJECTED" || qrParticipantStatusTerminal(participantStatus) {
		writeError(w, 409, "invalid_qr", "체크인할 수 없는 방문증입니다")
		return
	}
	singleUse, _ := s.getSetting(r.Context(), "visit.single_use_qr")
	if singleUse == "true" && usedAt != nil {
		writeError(w, 409, "already_used", "이미 체크인에 사용된 방문증입니다")
		return
	}
	if in.Method == "" {
		in.Method = "qr"
	}
	_, err = tx.Exec(r.Context(), `UPDATE qr_tokens SET used_at=COALESCE(used_at,now()) WHERE id=$1`, tokenID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE visitor_visits SET status='CHECKED_IN',checked_in_at=COALESCE(checked_in_at,now()),badge_no=NULLIF($2,'') WHERE id=$1`, participantID, in.BadgeNo)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE visits SET status='CHECKED_IN',updated_at=now() WHERE id=$1 AND status IN ('SCHEDULED','APPROVED','ARRIVED')`, visitID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,actor_user_id,lobby_id,method) VALUES($1,$2,'CHECKED_IN',NULLIF($3,''),NULLIF($4,''),$5)`, visitID, participantID, u.ID, in.LobbyID, in.Method)
	}
	if err == nil {
		err = s.queueNotificationEventTx(r.Context(), tx, visitID, participantID, "checked_in", now)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.metrics.checkIns.Add(1)
	s.audit(r.Context(), u.ID, "visit.checkin", "visitor_visit", participantID, clientIP(r), map[string]string{"visitId": visitID, "method": in.Method, "lobbyId": in.LobbyID})
	s.publishLobbyEvent("visitor.checked_in")
	writeJSON(w, 201, map[string]any{"visitorVisitId": participantID, "visitId": visitID, "checkedInAt": now})
}

func (s *Server) checkOut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		VisitorVisitID string `json:"visitorVisitId"`
		LobbyID        string `json:"lobbyId"`
		Method         string `json:"method"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Method == "" {
		in.Method = "lobby"
	}
	u, _ := userFrom(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var visitID string
	err = tx.QueryRow(r.Context(), `UPDATE visitor_visits vv SET status='CHECKED_OUT',checked_out_at=now() FROM visits v WHERE vv.id=$1 AND vv.status='CHECKED_IN' AND v.id=vv.visit_id AND ($2<>'lobby' OR cardinality($3::text[])=0 OR v.site_id=ANY($3::text[])) RETURNING vv.visit_id`, in.VisitorVisitID, u.Role, u.SiteScope).Scan(&visitID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO visit_events(visit_id,visitor_visit_id,event_type,actor_user_id,lobby_id,method) VALUES($1,$2,'CHECKED_OUT',NULLIF($3,''),NULLIF($4,''),$5)`, visitID, in.VisitorVisitID, u.ID, in.LobbyID, in.Method)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE visits SET status=CASE
			WHEN EXISTS(SELECT 1 FROM visitor_visits WHERE visit_id=$1 AND status='CHECKED_IN') THEN 'CHECKED_IN'
			WHEN EXISTS(SELECT 1 FROM visitor_visits WHERE visit_id=$1 AND status IN ('SCHEDULED','ARRIVED')) THEN 'SCHEDULED'
			ELSE 'CHECKED_OUT' END,updated_at=now() WHERE id=$1`, visitID)
	}
	if err == nil {
		err = s.queueNotificationEventTx(r.Context(), tx, visitID, in.VisitorVisitID, "checked_out", time.Now())
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 409, "checkout_conflict", "현재 방문 중인 방문자가 아니거나 이미 퇴실했습니다")
		return
	}
	s.metrics.checkOuts.Add(1)
	s.audit(r.Context(), u.ID, "visit.checkout", "visitor_visit", in.VisitorVisitID, clientIP(r), map[string]string{"visitId": visitID, "method": in.Method})
	s.publishLobbyEvent("visitor.checked_out")
	w.WriteHeader(http.StatusNoContent)
}

// lobbyListLimit caps one lobby page. The query fetches one extra row so the
// response can report that more visitors exist.
const lobbyListLimit = 300

func (s *Server) lobbyToday(w http.ResponseWriter, r *http.Request) {
	s.lobbyList(w, r, false)
}

func (s *Server) lobbyCurrent(w http.ResponseWriter, r *http.Request) {
	s.lobbyList(w, r, true)
}

func (s *Server) lobbyList(w http.ResponseWriter, r *http.Request, current bool) {
	u, _ := userFrom(r)
	where := `(v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date AND vv.status NOT IN ('CANCELLED','REJECTED')`
	if current {
		where = `vv.status='CHECKED_IN'`
	}
	rows, err := s.db.Query(r.Context(), `SELECT vv.id,v.id,p.name_encrypted,p.phone_encrypted,COALESCE(p.company,''),h.display_name,COALESCE(o.name,''),s.name,COALESCE(l.name,''),v.start_at,v.end_at,vv.status,vv.checked_in_at FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN visitors p ON p.id=vv.visitor_id JOIN users h ON h.id=v.host_user_id JOIN sites s ON s.id=v.site_id LEFT JOIN lobbies l ON l.id=v.lobby_id LEFT JOIN organizations o ON o.id=v.department_id WHERE `+where+` AND ($1<>'lobby' OR cardinality($2::text[])=0 OR v.site_id=ANY($2::text[])) AND ($3='' OR v.lobby_id=$3) ORDER BY COALESCE(vv.checked_in_at,v.start_at) DESC LIMIT 301`, u.Role, u.SiteScope, strings.TrimSpace(r.URL.Query().Get("lobby")))
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	scanned := 0
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	phoneSearch := normalizePhone(search)
	for rows.Next() {
		scanned++
		var participantID, visitID, nameEnc, phoneEnc, company, host, department, site, lobby, status string
		var start, end time.Time
		var checkedIn *time.Time
		if rows.Scan(&participantID, &visitID, &nameEnc, &phoneEnc, &company, &host, &department, &site, &lobby, &start, &end, &status, &checkedIn) == nil {
			name, phone := s.decryptOptional(nameEnc), s.decryptOptional(phoneEnc)
			matchesText := strings.Contains(strings.ToLower(name+" "+company+" "+host+" "+department), search)
			matchesPhone := phoneSearch != "" && strings.Contains(normalizePhone(phone), phoneSearch)
			if search != "" && !matchesText && !matchesPhone {
				continue
			}
			items = append(items, map[string]any{"visitorVisitId": participantID, "visitId": visitID, "visitor": name, "company": company, "host": host, "department": department, "site": site, "lobby": lobby, "startAt": start, "endAt": end, "status": status, "checkedInAt": checkedIn})
		}
	}
	var scheduled, currentCount, completed, noShow int
	_ = s.db.QueryRow(r.Context(), `SELECT
		count(*) FILTER(WHERE vv.status='SCHEDULED' AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date),
		count(*) FILTER(WHERE vv.status='CHECKED_IN'),
		count(*) FILTER(WHERE vv.status='CHECKED_OUT' AND (vv.checked_out_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date),
		count(*) FILTER(WHERE vv.status='NO_SHOW' AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date)
		FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites s ON s.id=v.site_id
		WHERE ($1<>'lobby' OR cardinality($2::text[])=0 OR v.site_id=ANY($2::text[]))`, u.Role, u.SiteScope).Scan(&scheduled, &currentCount, &completed, &noShow)
	counts := map[string]int{"scheduled": scheduled, "current": currentCount, "completed": completed, "noShow": noShow}
	// The list is capped; say so instead of silently dropping rows the lobby
	// operator would then look for in vain.
	truncated := scanned > lobbyListLimit
	if truncated && len(items) > lobbyListLimit {
		items = items[:lobbyListLimit]
	}
	writeJSON(w, 200, map[string]any{"counts": counts, "items": items, "truncated": truncated, "limit": lobbyListLimit})
}
