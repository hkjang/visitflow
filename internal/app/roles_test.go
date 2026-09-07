package app

import (
	"fmt"
	"net/http"
	"testing"
)

// Each role's navigation offers a set of screens, and each screen fires a fixed
// set of calls. This walks that map so a permission change that leaves a menu
// entry pointing at a forbidden endpoint fails here instead of in the console.
func TestRoleReachableEndpoints(t *testing.T) {
	env := newTestEnv(t)
	site := env.siteID()
	env.json(http.MethodPost, "/api/v1/visits", visitBody(site, nil), http.StatusCreated)
	// A department manager only ever opens visits their own department owns, so
	// model that: a department, a host inside it, and a visit awaiting approval.
	department := env.json(http.MethodPost, "/api/v1/admin/organizations", map[string]string{"name": "승인부서"}, http.StatusOK)
	departmentID := fmt.Sprint(department["id"])
	env.createLocalUser("dept-host", RoleUser, departmentID)
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"visit.approval_enabled": "true"}}, http.StatusOK)
	host := &testEnv{server: env.server, handler: env.handler, t: t}
	host.login("dept-host", testAdminPassword)
	pending := host.json(http.MethodPost, "/api/v1/visits", visitBody(site, nil), http.StatusCreated)
	visitID := fmt.Sprint(pending["id"])

	personal := []string{
		"/api/v1/auth/me", "/api/v1/reference-data", "/api/v1/dashboard", "/api/v1/visits",
		"/api/v1/visit-templates", "/api/v1/frequent-visitors", "/api/v1/guides",
		"/api/v1/api-keys", "/api/v1/api-key-policy", "/api/v1/profile/notifications",
	}
	lobby := []string{"/api/v1/lobby/today", "/api/v1/lobby/current", "/api/v1/lobby/roster"}
	securityOnly := []string{"/api/v1/admin/visitors", "/api/v1/admin/watchlist", "/api/v1/admin/visits.csv"}
	auditOnly := []string{"/api/v1/admin/audit-logs", "/api/v1/admin/audit-logs.csv"}
	adminOnly := []string{
		"/api/v1/admin/dashboard", "/api/v1/admin/metrics", "/api/v1/admin/statistics", "/api/v1/admin/statistics.csv",
		"/api/v1/admin/notifications", "/api/v1/admin/notification-apis", "/api/v1/admin/notification-rules",
		"/api/v1/admin/users", "/api/v1/admin/visit-types", "/api/v1/admin/kiosk-devices",
		"/api/v1/admin/guides", "/api/v1/settings", "/api/v1/settings/export",
	}

	join := func(sets ...[]string) []string {
		out := []string{}
		for _, set := range sets {
			out = append(out, set...)
		}
		return out
	}
	cases := []struct {
		role    string
		allowed []string
	}{
		{RoleUser, personal},
		{RoleDeptManager, join(personal, []string{"/api/v1/visits?status=PENDING_APPROVAL"})},
		{RoleLobby, join(personal, lobby)},
		{RoleSecurity, join(personal, lobby, securityOnly)},
		{RoleAuditor, join(personal, auditOnly)},
		{RoleAdmin, join(personal, lobby, securityOnly, auditOnly, adminOnly)},
	}
	for _, test := range cases {
		username := "role-" + test.role
		scope := ""
		if test.role == RoleDeptManager {
			scope = departmentID
		}
		env.createLocalUser(username, test.role, scope)
		if _, err := env.server.db.Exec(t.Context(), `UPDATE users SET must_change_password=false WHERE username=$1`, username); err != nil {
			t.Fatalf("clear temporary password: %v", err)
		}
		member := &testEnv{server: env.server, handler: env.handler, t: t}
		member.login(username, testAdminPassword)
		for _, path := range test.allowed {
			if response := member.do(http.MethodGet, path, nil); response.Code != http.StatusOK {
				t.Errorf("%s: GET %s returned %d (%s)", test.role, path, response.Code, response.Body.String())
			}
		}
	}

	// The approver's own path: the pending visit is listed, opens, and approves.
	manager := &testEnv{server: env.server, handler: env.handler, t: t}
	manager.login("role-"+RoleDeptManager, testAdminPassword)
	queue := manager.json(http.MethodGet, "/api/v1/visits?status=PENDING_APPROVAL", nil, http.StatusOK)
	listed := false
	for _, item := range queue["items"].([]any) {
		if fmt.Sprint(item.(map[string]any)["id"]) == visitID {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("the department's pending visit is not in its manager's queue: %v", queue)
	}
	manager.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	manager.json(http.MethodPost, "/api/v1/visits/"+visitID+"/approve", map[string]string{"reason": "확인"}, http.StatusNoContent)

	// And the boundary holds in the other direction: a plain user reaches none
	// of the privileged reads.
	plain := &testEnv{server: env.server, handler: env.handler, t: t}
	plain.login("role-"+RoleUser, testAdminPassword)
	for _, path := range join(lobby, securityOnly, auditOnly, adminOnly) {
		if response := plain.do(http.MethodGet, path, nil); response.Code != http.StatusForbidden {
			t.Errorf("user: GET %s returned %d, want 403", path, response.Code)
		}
	}
}
