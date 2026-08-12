package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}
type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	apiKeyAuth, _ := r.Context().Value(apiKeyAuthContextKey).(bool)
	scopes, _ := r.Context().Value(apiScopesContextKey).([]string)
	if !apiKeyAuth || !containsString(scopes, "mcp") {
		writeError(w, 403, "insufficient_scope", "mcp 범위가 있는 API 키가 필요합니다")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req mcpRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.JSONRPC != "2.0" {
		writeJSON(w, 400, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{-32600, "Invalid Request"}})
		return
	}
	switch req.Method {
	case "initialize":
		writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]string{"name": "VisitFlow", "version": s.version}, "instructions": "방문 신청부터 로비 현황·감사 통계까지 제공하는 VisitFlow MCP입니다. 개인정보는 기본 마스킹되고 REST와 동일한 사용자/부서/사업장 권한 범위가 적용됩니다."}})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": mcpTools()}})
	case "tools/call":
		s.mcpToolCall(w, r, req)
	default:
		writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{-32601, "Method not found"}})
	}
}

func mcpTools() []map[string]any {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	return []map[string]any{
		{"name": "search_visits", "description": "권한 범위 안에서 방문번호, 회사, 담당자와 상태로 방문 일정을 검색합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": stringProperty("검색어"), "status": stringProperty("방문 상태 코드"), "period": map[string]any{"type": "string", "enum": []string{"today", "upcoming", "past"}}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}}},
		{"name": "get_today_visitors", "description": "오늘 방문 예정자를 개인정보 마스킹 상태로 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}}},
		{"name": "get_current_visitors", "description": "현재 체크인 후 퇴실하지 않은 방문자를 조회합니다. 로비 또는 관리자 권한이 필요합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
		{"name": "get_visit", "description": "방문 ID 또는 방문번호로 상세 내용을 조회합니다. 전화번호는 마스킹됩니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"visit_id": stringProperty("방문 ID 또는 방문번호")}, "required": []string{"visit_id"}}},
		{"name": "create_visit", "description": "개인 방문 신청을 등록합니다. write 범위가 필요합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": stringProperty("사업장 ID"), "lobby_id": stringProperty("로비 ID"), "start_at": stringProperty("RFC3339 시작시간"), "end_at": stringProperty("RFC3339 종료시간"), "purpose": stringProperty("방문 목적"), "visitor_name": stringProperty("방문자 이름"), "visitor_phone": stringProperty("방문자 휴대전화"), "company": stringProperty("회사명"), "consent": map[string]any{"type": "boolean"}}, "required": []string{"site_id", "start_at", "end_at", "purpose", "visitor_name", "visitor_phone", "consent"}}},
		{"name": "cancel_visit", "description": "본인이 신청한 취소 가능한 방문을 취소하고 QR을 즉시 폐기합니다. write 범위가 필요합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"visit_id": stringProperty("방문 ID")}, "required": []string{"visit_id"}}},
		{"name": "search_visitor_history", "description": "회사 기준 방문 이력을 조회합니다. 이름과 전화번호는 마스킹됩니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"company": stringProperty("회사명"), "months": map[string]any{"type": "integer", "minimum": 1, "maximum": 60}}, "required": []string{"company"}}},
		{"name": "get_lobby_status", "description": "오늘 예정·현재 방문중·퇴실·미방문 집계를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
		{"name": "get_visit_statistics", "description": "기간 방문 통계를 조회합니다. 관리자 권한이 필요합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"days": map[string]any{"type": "integer", "minimum": 1, "maximum": 366}}}},
	}
}

func (s *Server) mcpToolCall(w http.ResponseWriter, r *http.Request, req mcpRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
		writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{-32602, "Invalid params"}})
		return
	}
	result, err := s.executeMCPTool(r, params.Name, params.Arguments)
	if err != nil {
		writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true}})
		return
	}
	raw, _ := json.Marshal(result)
	writeJSON(w, 200, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": []map[string]string{{"type": "text", "text": string(raw)}}, "structuredContent": result, "isError": false}})
}

func (s *Server) executeMCPTool(r *http.Request, name string, args map[string]any) (any, error) {
	u, _ := userFrom(r)
	limit := intArg(args, "limit", 20, 1, 100)
	writeAllowed := func() bool {
		scopes, _ := r.Context().Value(apiScopesContextKey).([]string)
		return len(scopes) == 0 || containsString(scopes, "write")
	}
	switch name {
	case "search_visits":
		items, err := s.queryVisits(r.Context(), u, stringArg(args, "status"), stringArg(args, "period"), stringArg(args, "query"), limit)
		return map[string]any{"items": items}, err
	case "get_today_visitors":
		items, err := s.queryVisits(r.Context(), u, "", "today", "", limit)
		return map[string]any{"items": items}, err
	case "get_current_visitors":
		if !u.CanManageLobby() && !u.CanAudit() {
			return nil, errMCP("로비 또는 감사 권한이 필요합니다")
		}
		rows, err := s.db.Query(r.Context(), `SELECT vv.id,p.name_encrypted,COALESCE(p.company,''),h.display_name,si.name,vv.checked_in_at FROM visitor_visits vv JOIN visitors p ON p.id=vv.visitor_id JOIN visits v ON v.id=vv.visit_id JOIN users h ON h.id=v.host_user_id JOIN sites si ON si.id=v.site_id WHERE vv.status='CHECKED_IN' AND ($1<>'lobby' OR cardinality($2::text[])=0 OR v.site_id=ANY($2::text[])) ORDER BY vv.checked_in_at DESC LIMIT 200`, u.Role, u.SiteScope)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, nameEnc, company, host, site string
			var checked *time.Time
			_ = rows.Scan(&id, &nameEnc, &company, &host, &site, &checked)
			items = append(items, map[string]any{"visitorVisitId": id, "visitor": maskName(s.decryptOptional(nameEnc)), "company": company, "host": host, "site": site, "checkedInAt": checked})
		}
		return map[string]any{"items": items, "count": len(items)}, nil
	case "get_visit":
		id := stringArg(args, "visit_id")
		if id == "" {
			return nil, errMCP("visit_id는 필수입니다")
		}
		items, err := s.queryVisits(r.Context(), u, "", "", id, 5)
		if err != nil || len(items) == 0 {
			return nil, errMCP("방문을 찾을 수 없습니다")
		}
		participants, err := s.visitParticipants(r.Context(), items[0].ID, false)
		if err != nil {
			return nil, err
		}
		for _, p := range participants {
			p["name"] = maskName(fmt.Sprint(p["name"]))
			delete(p, "email")
			delete(p, "vehicle")
		}
		return map[string]any{"visit": items[0], "visitors": participants}, nil
	case "create_visit":
		if !writeAllowed() {
			return nil, errMCP("write 범위가 필요합니다")
		}
		start, err1 := time.Parse(time.RFC3339, stringArg(args, "start_at"))
		end, err2 := time.Parse(time.RFC3339, stringArg(args, "end_at"))
		consent, _ := args["consent"].(bool)
		if err1 != nil || err2 != nil {
			return nil, errMCP("start_at과 end_at은 RFC3339 형식이어야 합니다")
		}
		input := VisitInput{SiteID: stringArg(args, "site_id"), LobbyID: stringArg(args, "lobby_id"), StartAt: start, EndAt: end, Purpose: stringArg(args, "purpose"), Visitors: []VisitorInput{{Name: stringArg(args, "visitor_name"), Phone: stringArg(args, "visitor_phone"), Company: stringArg(args, "company"), Consent: consent}}}
		return s.createVisitRecord(r.Context(), r, u, input, "mcp", false)
	case "cancel_visit":
		if !writeAllowed() {
			return nil, errMCP("write 범위가 필요합니다")
		}
		id := stringArg(args, "visit_id")
		tx, err := s.db.Begin(r.Context())
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(r.Context())
		tag, err := tx.Exec(r.Context(), `UPDATE visits SET status='CANCELLED',cancelled_at=now(),updated_at=now() WHERE id=$1 AND (host_user_id=$2 OR $3) AND status IN ('PENDING_APPROVAL','APPROVED','SCHEDULED')`, id, u.ID, u.IsAdmin())
		if err != nil || tag.RowsAffected() == 0 {
			return nil, errMCP("취소 가능한 방문이 아니거나 권한이 없습니다")
		}
		_, _ = tx.Exec(r.Context(), `UPDATE visitor_visits SET status='CANCELLED' WHERE visit_id=$1; UPDATE qr_tokens SET revoked_at=now() WHERE visitor_visit_id IN (SELECT id FROM visitor_visits WHERE visit_id=$1) AND revoked_at IS NULL`, id)
		if err = tx.Commit(r.Context()); err != nil {
			return nil, err
		}
		s.audit(r.Context(), u.ID, "visit.cancel", "visit", id, r.RemoteAddr, map[string]string{"source": "mcp"})
		s.publishLobbyEvent("visit.cancelled")
		return map[string]any{"cancelled": true, "visitId": id}, nil
	case "search_visitor_history":
		company := strings.TrimSpace(stringArg(args, "company"))
		months := intArg(args, "months", 6, 1, 60)
		if company == "" {
			return nil, errMCP("company는 필수입니다")
		}
		departmentID := ""
		if u.DepartmentID != nil {
			departmentID = *u.DepartmentID
		}
		rows, err := s.db.Query(r.Context(), `SELECT p.name_encrypted,p.company,v.request_no,v.start_at,v.status,h.display_name FROM visitors p JOIN visitor_visits vv ON vv.visitor_id=p.id JOIN visits v ON v.id=vv.visit_id JOIN users h ON h.id=v.host_user_id WHERE lower(p.company)=lower($1) AND v.start_at>=now()-($2::int*interval '1 month') AND ($4 IN ('admin','super_admin','security','auditor') OR ($4='lobby' AND (cardinality($6::text[])=0 OR v.site_id=ANY($6::text[]))) OR ($4='dept_manager' AND v.department_id=NULLIF($5,'')) OR v.host_user_id=$3) ORDER BY v.start_at DESC LIMIT 200`, company, months, u.ID, u.Role, departmentID, u.SiteScope)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var nameEnc, companyName, requestNo, status, host string
			var start time.Time
			_ = rows.Scan(&nameEnc, &companyName, &requestNo, &start, &status, &host)
			items = append(items, map[string]any{"visitor": maskName(s.decryptOptional(nameEnc)), "company": companyName, "requestNo": requestNo, "startAt": start, "status": status, "host": host})
		}
		return map[string]any{"items": items, "count": len(items), "months": months}, nil
	case "get_lobby_status":
		if !u.CanManageLobby() && !u.CanAudit() {
			return nil, errMCP("로비 또는 감사 권한이 필요합니다")
		}
		var scheduled, current, completed, noShow int
		err := s.db.QueryRow(r.Context(), `SELECT
			count(*) FILTER(WHERE vv.status='SCHEDULED' AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date),
			count(*) FILTER(WHERE vv.status='CHECKED_IN'),
			count(*) FILTER(WHERE vv.status='CHECKED_OUT' AND (vv.checked_out_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date),
			count(*) FILTER(WHERE vv.status='NO_SHOW' AND (v.start_at AT TIME ZONE s.timezone)::date=(now() AT TIME ZONE s.timezone)::date)
			FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites s ON s.id=v.site_id
			WHERE ($1<>'lobby' OR cardinality($2::text[])=0 OR v.site_id=ANY($2::text[]))`, u.Role, u.SiteScope).Scan(&scheduled, &current, &completed, &noShow)
		return map[string]any{"scheduled": scheduled, "current": current, "completed": completed, "noShow": noShow}, err
	case "get_visit_statistics":
		if !u.IsAdmin() {
			return nil, errMCP("관리자 권한이 필요합니다")
		}
		days := intArg(args, "days", 30, 1, 366)
		var scheduled, checked int
		err := s.db.QueryRow(r.Context(), `SELECT count(vv.id),count(vv.id) FILTER(WHERE vv.status IN ('CHECKED_IN','CHECKED_OUT')) FROM visits v JOIN visitor_visits vv ON vv.visit_id=v.id WHERE v.start_at>=CURRENT_DATE-$1::int`, days).Scan(&scheduled, &checked)
		return map[string]any{"days": days, "scheduled": scheduled, "checkedIn": checked}, err
	default:
		return nil, errMCP("알 수 없는 도구입니다: " + name)
	}
}

type errMCP string

func (e errMCP) Error() string                      { return string(e) }
func stringArg(m map[string]any, key string) string { v, _ := m[key].(string); return v }
func intArg(m map[string]any, key string, fallback, min, max int) int {
	v, ok := m[key].(float64)
	if !ok {
		return fallback
	}
	n := int(v)
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
