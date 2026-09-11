package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Every list endpoint scans its rows by hand, so a column added to the query
// without a matching Scan target only fails once a row exists. Several lists
// were only ever read while empty. This seeds one row into each and reads them
// all back, exercising the scan path and the fields the console renders.
func TestEveryListReturnsItsRows(t *testing.T) {
	env := newTestEnv(t)
	site := env.siteID()
	reference := env.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK)
	lobbyID := fmt.Sprint(reference["lobbies"].([]any)[0].(map[string]any)["id"])

	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(site, map[string]any{"lobbyId": lobbyID}), http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/checkins", map[string]string{"token": passTokenFrom(t, created)}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/admin/notification-apis", map[string]any{
		"name": "게이트웨이", "channel": "sms", "baseUrl": "https://sms.test.local", "path": "/send",
		"method": "POST", "requestFormat": "json",
		"parameters": map[string]string{"to": "{{recipient}}", "text": "{{message}}", "key": "{{idempotencyKey}}"},
		"headers":    map[string]string{"Authorization": "Bearer secret"},
		"secretKeys": []string{"headers.Authorization"},
	}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/admin/notification-rules", map[string]any{
		"name": "도착 안내", "event": "checked_in", "audience": "host", "channel": "sms",
		"templateKey": "host_arrival", "bodyTemplate": "{{visitor}} 도착",
	}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/admin/watchlist", map[string]any{"name": "제한", "phone": "010-9999-0000", "company": "제한상사", "reason": "보안"}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/admin/kiosk-devices", map[string]any{"name": "키오스크", "siteId": site, "lobbyId": lobbyID, "validDays": 30}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/admin/guides", map[string]any{"title": "안내", "category": "일반", "content": "본문", "published": true, "pinned": true}, http.StatusCreated)
	frequent := env.json(http.MethodPost, "/api/v1/frequent-visitors", map[string]any{"name": "단골", "phone": "010-3333-4444", "company": "협력사", "consent": true, "equipment": []string{"노트북"}}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/visit-templates", map[string]any{"name": "정기 점검", "payload": map[string]any{"purpose": "점검"}, "frequentVisitorIds": []string{fmt.Sprint(frequent["id"])}}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "통합 키"}, http.StatusCreated)
	env.json(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "list-user", "displayName": "목록 사용자", "role": RoleLobby, "siteScope": []string{site}}, http.StatusCreated)

	for _, check := range []struct {
		path   string
		fields []string
	}{
		{"/api/v1/admin/notification-apis", []string{"id", "name", "channel", "baseUrl", "path", "method", "requestFormat", "headers", "parameters", "secretKeys", "timeoutSeconds", "enabled"}},
		{"/api/v1/admin/notification-rules", []string{"id", "name", "event", "audience", "channel", "offsetMinutes", "templateKey", "bodyTemplate", "subjectTemplate", "locale", "enabled"}},
		{"/api/v1/admin/watchlist", []string{"id", "name", "company", "reason", "startsAt", "active", "createdAt"}},
		{"/api/v1/admin/kiosk-devices", []string{"id", "name", "siteId", "siteName", "lobbyName", "prefix", "active", "createdAt"}},
		{"/api/v1/admin/guides", []string{"id", "title", "category", "content", "published", "pinned", "authorName"}},
		{"/api/v1/guides", []string{"id", "title", "category", "excerpt", "published", "pinned", "authorName"}},
		{"/api/v1/frequent-visitors", []string{"id", "name", "phone", "company", "equipment", "consent", "templateCount"}},
		{"/api/v1/visit-templates", []string{"id", "name", "payload", "frequentVisitorIds", "frequentVisitorCount"}},
		{"/api/v1/api-keys", []string{"id", "name", "prefix", "scopes", "version", "createdAt", "expiresAt"}},
		{"/api/v1/admin/users", []string{"id", "username", "displayName", "role", "source", "active", "siteScope", "activeSessions"}},
		{"/api/v1/admin/visitors", []string{"id", "name", "phone", "company", "visitCount"}},
		{"/api/v1/admin/notifications", []string{"id", "channel", "templateKey", "status", "attempts", "recipient", "createdAt"}},
		{"/api/v1/admin/audit-logs", []string{"id", "actor", "action", "resourceType", "createdAt"}},
		{"/api/v1/admin/visit-types", []string{"id", "code", "name", "requiresNda", "active", "sortOrder"}},
		{"/api/v1/lobby/today", []string{"visitorVisitId", "visitId", "visitor", "host", "site", "lobby", "startAt", "status"}},
		{"/api/v1/lobby/current", []string{"visitorVisitId", "visitor", "checkedInAt"}},
		{"/api/v1/lobby/roster", []string{"site", "visitor", "host", "checkedInAt"}},
		{"/api/v1/visits", []string{"id", "requestNo", "hostName", "siteName", "lobbyName", "startAt", "status", "visitorCount", "primaryVisitor"}},
	} {
		body := env.json(http.MethodGet, check.path, nil, http.StatusOK)
		items, _ := body["items"].([]any)
		if len(items) == 0 {
			t.Errorf("%s returned no rows, so its scan path stayed untested", check.path)
			continue
		}
		row, _ := items[0].(map[string]any)
		for _, field := range check.fields {
			if _, present := row[field]; !present {
				t.Errorf("%s row is missing %q: %v", check.path, field, row)
			}
		}
	}

	// A configured secret must never be listed in the clear.
	apis := env.json(http.MethodGet, "/api/v1/admin/notification-apis", nil, http.StatusOK)
	headers := apis["items"].([]any)[0].(map[string]any)["headers"].(map[string]any)
	if fmt.Sprint(headers["Authorization"]) != maskedNotificationSecret {
		t.Errorf("secret header listed unmasked: %v", headers)
	}
}

// The remaining endpoints no other test reaches. They are small, but a broken
// one is invisible until an operator hits it.
func TestSmallEndpointsRespond(t *testing.T) {
	env := newTestEnv(t)

	config := env.json(http.MethodGet, "/api/v1/auth/config", nil, http.StatusOK)
	if config["localEnabled"] != true || config["serviceName"] != "VisitFlow" {
		t.Fatalf("auth config: %v", config)
	}

	spec := env.json(http.MethodGet, "/api/v1/openapi.json", nil, http.StatusOK)
	paths, _ := spec["paths"].(map[string]any)
	if spec["openapi"] != "3.1.0" || len(paths) == 0 {
		t.Fatalf("openapi document: %v", spec["openapi"])
	}
	// Every documented path must exist on the router, or the document sends
	// integrators to endpoints that are not there.
	routed := env.json(http.MethodGet, "/api/v1/version", nil, http.StatusOK)
	_ = routed
	for path := range paths {
		if strings.HasPrefix(path, "/img/") {
			continue
		}
		probe := env.do(http.MethodGet, "/api/v1"+strings.ReplaceAll(strings.ReplaceAll(path, "{", "x"), "}", ""), nil)
		if probe.Code == http.StatusNotFound && strings.Contains(probe.Body.String(), "endpoint_not_found") {
			// GET may simply not be the documented verb; only a completely
			// unrouted path answers endpoint_not_found for every verb.
			for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
				again := env.do(method, "/api/v1"+strings.ReplaceAll(strings.ReplaceAll(path, "{", "x"), "}", ""), map[string]any{})
				if again.Code != http.StatusNotFound || !strings.Contains(again.Body.String(), "endpoint_not_found") {
					probe = again
					break
				}
			}
			if probe.Code == http.StatusNotFound && strings.Contains(probe.Body.String(), "endpoint_not_found") {
				t.Errorf("openapi documents %s, which the router does not serve", path)
			}
		}
	}

	if oidc := env.do(http.MethodPost, "/api/v1/settings/oidc/test", map[string]any{}); oidc.Code != http.StatusBadRequest {
		t.Fatalf("oidc test without an issuer returned %d: %s", oidc.Code, oidc.Body.String())
	}

	bulk := env.json(http.MethodPost, "/api/v1/admin/notifications/retry-failed", map[string]any{}, http.StatusOK)
	if _, present := bulk["queued"]; !present {
		t.Fatalf("bulk retry response: %v", bulk)
	}

	// Logging out invalidates the session it was called with.
	env.json(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent)
	if after := env.do(http.MethodGet, "/api/v1/auth/me", nil); after.Code != http.StatusUnauthorized {
		t.Fatalf("session survived logout: %d", after.Code)
	}
}
