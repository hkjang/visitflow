package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hkjang/visitflow/internal/database"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	return string(hash), err
}

// The integration suite exercises the flows that carry the product's security
// promises — single-use QR, approval gating, consent capture, throttling — end
// to end against a real PostgreSQL. Set VISITFLOW_TEST_DSN to a server the test
// may create and drop databases on; without it the suite skips.
const testAdminPassword = "integration-test-password"

type testEnv struct {
	server  *Server
	handler http.Handler
	cookies []*http.Cookie
	csrf    string
	t       *testing.T
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return fmt.Sprintf("%x", b)
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	adminDSN := strings.TrimSpace(os.Getenv("VISITFLOW_TEST_DSN"))
	if adminDSN == "" {
		t.Skip("VISITFLOW_TEST_DSN is not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Fatalf("ping admin database: %v", err)
	}
	name := "visitflow_test_" + randomSuffix(t)
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		adminPool.Close()
		t.Fatalf("create test database: %v", err)
	}
	parsed, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	parsed.Path = "/" + name
	pool, err := database.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := adminPool.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Logf("drop test database: %v", err)
		}
		adminPool.Close()
	})
	server := newServerForPool(t, pool)
	env := &testEnv{server: server, handler: server.Routes(), t: t}
	env.login("admin", testAdminPassword)
	return env
}

func newServerForPool(t *testing.T, pool *pgxpool.Pool) *Server {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random key: %v", err)
	}
	keyring, err := platform.NewKeyringFromSecret(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	server := NewServer(pool, keyring, slog.New(slog.NewTextHandler(io.Discard, nil)),
		fstest.MapFS{"index.html": {Data: []byte("<html><head></head><body>spa</body></html>")}}, "test", "test", "test")
	ctx := context.Background()
	if err := server.EnsureEncryptionKey(ctx); err != nil {
		t.Fatalf("encryption key: %v", err)
	}
	if err := server.EnsureBootstrapAdmin(ctx, "admin", testAdminPassword); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	return server
}

func (e *testEnv) do(method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("encode body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	request := httptest.NewRequest(method, path, reader)
	request.RemoteAddr = "10.0.0.1:5000"
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if e.csrf != "" {
		request.Header.Set("X-CSRF-Token", e.csrf)
	}
	for _, cookie := range e.cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		e.cookies = append(e.cookies, cookie)
	}
	return response
}

func (e *testEnv) json(method, path string, body any, wantStatus int) map[string]any {
	e.t.Helper()
	response := e.do(method, path, body)
	if response.Code != wantStatus {
		e.t.Fatalf("%s %s returned %d (want %d): %s", method, path, response.Code, wantStatus, response.Body.String())
	}
	if response.Body.Len() == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

func (e *testEnv) login(username, password string) {
	e.t.Helper()
	e.cookies = nil
	e.csrf = ""
	if response := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": password}); response.Code != http.StatusOK {
		e.t.Fatalf("login failed: %d %s", response.Code, response.Body.String())
	}
	me := e.json(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	csrf, _ := me["csrfToken"].(string)
	if csrf == "" {
		e.t.Fatal("session did not return a CSRF token")
	}
	e.csrf = csrf
}

func (e *testEnv) siteID() string {
	e.t.Helper()
	reference := e.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK)
	sites, _ := reference["sites"].([]any)
	if len(sites) == 0 {
		e.t.Fatal("no seeded site")
	}
	site, _ := sites[0].(map[string]any)
	return fmt.Sprint(site["id"])
}

func (e *testEnv) visitTypeID(code string) string {
	e.t.Helper()
	types := e.json(http.MethodGet, "/api/v1/admin/visit-types", nil, http.StatusOK)
	items, _ := types["items"].([]any)
	for _, item := range items {
		entry, _ := item.(map[string]any)
		if fmt.Sprint(entry["code"]) == code {
			return fmt.Sprint(entry["id"])
		}
	}
	e.t.Fatalf("visit type %s not seeded", code)
	return ""
}

func visitBody(siteID string, extra map[string]any) map[string]any {
	body := map[string]any{
		"siteId":  siteID,
		"startAt": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
		"endAt":   time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		"purpose": "통합 테스트 방문",
		"visitors": []map[string]any{
			{"name": "김방문", "phone": "010-1234-5678", "company": "테스트상사", "consent": true},
		},
	}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func passTokenFrom(t *testing.T, created map[string]any) string {
	t.Helper()
	urls, _ := created["passUrls"].([]any)
	if len(urls) == 0 {
		t.Fatalf("visit did not return a pass url: %v", created)
	}
	value := fmt.Sprint(urls[0])
	index := strings.LastIndex(value, "/q/")
	if index < 0 {
		t.Fatalf("unexpected pass url %q", value)
	}
	return value[index+len("/q/"):]
}

func TestMigrationsAreIdempotentAndVersioned(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	before, err := database.SchemaVersion(ctx, env.server.db)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if expected := database.ExpectedSchemaVersion(); before != expected {
		t.Fatalf("applied version %d does not match shipped version %d", before, expected)
	}
	if err := database.Migrate(ctx, env.server.db); err != nil {
		t.Fatalf("re-running migrations failed: %v", err)
	}
	after, err := database.SchemaVersion(ctx, env.server.db)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if after != before {
		t.Fatalf("re-running migrations changed the version from %d to %d", before, after)
	}
}

func TestEncryptionKeyMismatchIsRejectedAtStartup(t *testing.T) {
	env := newTestEnv(t)
	other := newServerWithDifferentKey(t, env.server.db)
	if err := other.EnsureEncryptionKey(context.Background()); err == nil {
		t.Fatal("a mismatched ENCRYPTION_KEY was accepted")
	}
}

func newServerWithDifferentKey(t *testing.T, pool *pgxpool.Pool) *Server {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random key: %v", err)
	}
	keyring, err := platform.NewKeyringFromSecret(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return NewServer(pool, keyring, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{}, "test", "test", "test")
}

func TestLoginThrottleLocksAfterRepeatedFailures(t *testing.T) {
	env := newTestEnv(t)
	env.cookies, env.csrf = nil, ""
	for attempt := 0; attempt < 5; attempt++ {
		if response := env.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}); response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d returned %d, want 401", attempt+1, response.Code)
		}
	}
	locked := env.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testAdminPassword})
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("account was not locked after five failures: %d %s", locked.Code, locked.Body.String())
	}
	if locked.Header().Get("Retry-After") == "" {
		t.Fatal("lockout response is missing Retry-After")
	}
}

func TestVisitLifecycleRejectsQRReuse(t *testing.T) {
	env := newTestEnv(t)
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(env.siteID(), nil), http.StatusCreated)
	token := passTokenFrom(t, created)

	verified := env.json(http.MethodPost, "/api/v1/qr/verify", map[string]string{"token": token}, http.StatusOK)
	if verified["valid"] != true {
		t.Fatalf("QR verification did not succeed: %v", verified)
	}
	env.json(http.MethodPost, "/api/v1/checkins", map[string]string{"token": token, "method": "qr"}, http.StatusCreated)

	replay := env.do(http.MethodPost, "/api/v1/checkins", map[string]string{"token": token, "method": "qr"})
	if replay.Code != http.StatusConflict {
		t.Fatalf("single-use QR was accepted twice: %d %s", replay.Code, replay.Body.String())
	}

	current := env.json(http.MethodGet, "/api/v1/lobby/current", nil, http.StatusOK)
	items, _ := current["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one checked-in visitor, got %d", len(items))
	}
	entry, _ := items[0].(map[string]any)
	participantID := fmt.Sprint(entry["visitorVisitId"])

	roster := env.json(http.MethodGet, "/api/v1/lobby/roster", nil, http.StatusOK)
	if fmt.Sprint(roster["count"]) != "1" {
		t.Fatalf("emergency roster did not list the visitor: %v", roster)
	}

	env.json(http.MethodPost, "/api/v1/checkouts", map[string]string{"visitorVisitId": participantID, "method": "lobby"}, http.StatusNoContent)
	afterCheckout := env.json(http.MethodGet, "/api/v1/lobby/roster", nil, http.StatusOK)
	if fmt.Sprint(afterCheckout["count"]) != "0" {
		t.Fatalf("roster still lists a visitor after checkout: %v", afterCheckout)
	}
}

func TestVisitTypeChecklistIsEnforced(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	contractor := env.visitTypeID("CONTRACTOR")

	missing := env.do(http.MethodPost, "/api/v1/visits", visitBody(siteID, map[string]any{"visitTypeId": contractor}))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("contractor visit without acknowledgements returned %d: %s", missing.Code, missing.Body.String())
	}

	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, map[string]any{
		"visitTypeId": contractor,
		"checklist":   map[string]bool{"nda": true, "safetyBriefing": true},
	}), http.StatusCreated)
	if created["status"] != "PENDING_APPROVAL" {
		t.Fatalf("a visit type requiring approval did not enter the workflow: %v", created)
	}

	visitID := fmt.Sprint(created["id"])
	env.json(http.MethodPost, "/api/v1/visits/"+visitID+"/approve", map[string]string{"reason": "테스트 승인"}, http.StatusNoContent)
	detail := env.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	visit, _ := detail["visit"].(map[string]any)
	if visit["status"] != "SCHEDULED" {
		t.Fatalf("approved visit is in status %v", visit["status"])
	}
}

func TestSelfRegistrationRecordsVisitorConsent(t *testing.T) {
	env := newTestEnv(t)
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(env.siteID(), nil), http.StatusCreated)
	visitID := fmt.Sprint(created["id"])
	detail := env.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	visitors, _ := detail["visitors"].([]any)
	participant, _ := visitors[0].(map[string]any)
	participantID := fmt.Sprint(participant["id"])

	invitation := env.json(http.MethodPost, "/api/v1/visitor-visits/"+participantID+"/invitation", map[string]any{}, http.StatusCreated)
	registrationURL := fmt.Sprint(invitation["registrationUrl"])
	index := strings.LastIndex(registrationURL, "/r/")
	if index < 0 {
		t.Fatalf("unexpected registration url %q", registrationURL)
	}
	token := registrationURL[index+len("/r/"):]

	// The visitor's own browser carries neither session nor CSRF token.
	public := &testEnv{server: env.server, handler: env.handler, t: t}
	page := public.json(http.MethodGet, "/api/v1/public/registrations/"+token+"?lang=en", nil, http.StatusOK)
	if page["locale"] != "en" {
		t.Fatalf("requested locale was not honoured: %v", page["locale"])
	}
	public.json(http.MethodPost, "/api/v1/public/registrations/"+token, map[string]any{
		"name": "Kim Visitor", "company": "테스트상사", "locale": "en", "consent": true,
	}, http.StatusOK)

	reused := public.do(http.MethodPost, "/api/v1/public/registrations/"+token, map[string]any{"name": "Kim Visitor", "consent": true})
	if reused.Code != http.StatusNotFound {
		t.Fatalf("a completed registration link was reusable: %d", reused.Code)
	}

	var source, policyVersion, locale string
	if err := env.server.db.QueryRow(context.Background(),
		`SELECT source,policy_version,locale FROM consent_records WHERE visitor_visit_id=$1 ORDER BY consented_at DESC LIMIT 1`, participantID).
		Scan(&source, &policyVersion, &locale); err != nil {
		t.Fatalf("consent record missing: %v", err)
	}
	if source != "self" || locale != "en" || policyVersion == "" {
		t.Fatalf("unexpected consent record: source=%s locale=%s policy=%s", source, locale, policyVersion)
	}
}

func TestVisitListPaginatesWithCursor(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	for index := 0; index < 3; index++ {
		body := visitBody(siteID, nil)
		body["startAt"] = time.Now().Add(time.Duration(index+1) * time.Hour).UTC().Format(time.RFC3339)
		body["endAt"] = time.Now().Add(time.Duration(index+3) * time.Hour).UTC().Format(time.RFC3339)
		env.json(http.MethodPost, "/api/v1/visits", body, http.StatusCreated)
	}
	first := env.json(http.MethodGet, "/api/v1/visits?limit=2", nil, http.StatusOK)
	firstItems, _ := first["items"].([]any)
	if len(firstItems) != 2 || first["hasMore"] != true {
		t.Fatalf("first page did not paginate: %v", first)
	}
	cursor := fmt.Sprint(first["nextCursor"])
	second := env.json(http.MethodGet, "/api/v1/visits?limit=2&cursor="+url.QueryEscape(cursor), nil, http.StatusOK)
	secondItems, _ := second["items"].([]any)
	if len(secondItems) != 1 || second["hasMore"] != false {
		t.Fatalf("second page did not finish the list: %v", second)
	}
	firstID := fmt.Sprint(firstItems[0].(map[string]any)["id"])
	if fmt.Sprint(secondItems[0].(map[string]any)["id"]) == firstID {
		t.Fatal("cursor paging repeated a row")
	}
	if bad := env.do(http.MethodGet, "/api/v1/visits?cursor=not-a-cursor", nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor returned %d", bad.Code)
	}
}

func TestKioskDeviceCanCheckVisitorsIn(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, nil), http.StatusCreated)
	token := passTokenFrom(t, created)

	device := env.json(http.MethodPost, "/api/v1/admin/kiosk-devices", map[string]any{"name": "1층 키오스크", "siteId": siteID, "validDays": 30}, http.StatusCreated)
	deviceToken := fmt.Sprint(device["token"])

	kiosk := &testEnv{server: env.server, handler: env.handler, t: t}
	enrolled := kiosk.json(http.MethodPost, "/api/v1/kiosk/enroll", map[string]string{"token": deviceToken}, http.StatusOK)
	kiosk.csrf = fmt.Sprint(enrolled["csrfToken"])
	kiosk.json(http.MethodPost, "/api/v1/checkins", map[string]string{"token": token, "method": "kiosk"}, http.StatusCreated)

	// A kiosk token must not reach anything outside the lobby group.
	if forbidden := kiosk.do(http.MethodGet, "/api/v1/admin/users", nil); forbidden.Code != http.StatusUnauthorized {
		t.Fatalf("kiosk cookie reached an admin endpoint: %d", forbidden.Code)
	}

	env.json(http.MethodDelete, "/api/v1/admin/kiosk-devices/"+fmt.Sprint(device["id"]), nil, http.StatusNoContent)
	revoked := kiosk.do(http.MethodGet, "/api/v1/lobby/current", nil)
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked kiosk device still had access: %d", revoked.Code)
	}
}

// createLocalUser inserts a password user with the given role and department so
// role- and delegation-dependent flows can be exercised end to end.
func (e *testEnv) createLocalUser(username, role, departmentID string) string {
	e.t.Helper()
	hash, err := bcryptHash(testAdminPassword)
	if err != nil {
		e.t.Fatalf("hash: %v", err)
	}
	id := newID()
	if _, err := e.server.db.Exec(context.Background(), `INSERT INTO users(id,username,password_hash,display_name,role,source,department_id) VALUES($1,$2,$3,$2,$4,'local',NULLIF($5,''))`,
		id, username, hash, role, departmentID); err != nil {
		e.t.Fatalf("create user %s: %v", username, err)
	}
	return id
}

func TestDelegateOfDepartmentManagerCanApprove(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	department := env.json(http.MethodPost, "/api/v1/admin/organizations", map[string]string{"name": "연구소"}, http.StatusOK)
	departmentID := fmt.Sprint(department["id"])
	managerID := env.createLocalUser("manager", RoleDeptManager, departmentID)
	env.createLocalUser("deputy", RoleUser, "")
	hostID := env.createLocalUser("host", RoleUser, departmentID)

	// Turn the approval workflow on and file a visit in the manager's department.
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"visit.approval_enabled": "true"}}, http.StatusOK)
	hostEnv := &testEnv{server: env.server, handler: env.handler, t: t}
	hostEnv.login("host", testAdminPassword)
	created := hostEnv.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, nil), http.StatusCreated)
	visitID := fmt.Sprint(created["id"])
	if created["status"] != "PENDING_APPROVAL" {
		t.Fatalf("visit did not enter approval: %v", created)
	}
	_ = hostID

	// Before any delegation the deputy is a plain user without approval rights.
	deputy := &testEnv{server: env.server, handler: env.handler, t: t}
	deputy.login("deputy", testAdminPassword)
	if denied := deputy.do(http.MethodPost, "/api/v1/visits/"+visitID+"/approve", map[string]string{"reason": "x"}); denied.Code != http.StatusForbidden {
		t.Fatalf("undelegated user approved a visit: %d", denied.Code)
	}

	// The manager delegates to the deputy for a day.
	manager := &testEnv{server: env.server, handler: env.handler, t: t}
	manager.login("manager", testAdminPassword)
	deputyID := ""
	if err := env.server.db.QueryRow(context.Background(), `SELECT id FROM users WHERE username='deputy'`).Scan(&deputyID); err != nil {
		t.Fatalf("deputy id: %v", err)
	}
	manager.json(http.MethodPatch, "/api/v1/profile", map[string]any{"displayName": "manager", "delegateUserId": deputyID, "delegateUntil": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}, http.StatusNoContent)
	_ = managerID

	deputy.login("deputy", testAdminPassword)
	me := deputy.json(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if user, _ := me["user"].(map[string]any); user["approvalDelegate"] != true {
		t.Fatalf("delegate flag missing from /auth/me: %v", me["user"])
	}
	pending := deputy.json(http.MethodGet, "/api/v1/visits?status=PENDING_APPROVAL", nil, http.StatusOK)
	if items, _ := pending["items"].([]any); len(items) != 1 {
		t.Fatalf("delegate does not see the department's pending visit: %v", pending)
	}
	deputy.json(http.MethodPost, "/api/v1/visits/"+visitID+"/approve", map[string]string{"reason": "대리 승인"}, http.StatusNoContent)
	var status string
	if err := env.server.db.QueryRow(context.Background(), `SELECT status FROM visits WHERE id=$1`, visitID).Scan(&status); err != nil || status != "SCHEDULED" {
		t.Fatalf("visit status after delegate approval: %s %v", status, err)
	}
}

func TestWalkInWithoutHostIsRejected(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	device := env.json(http.MethodPost, "/api/v1/admin/kiosk-devices", map[string]any{"name": "kiosk", "siteId": siteID, "validDays": 1}, http.StatusCreated)
	kiosk := &testEnv{server: env.server, handler: env.handler, t: t}
	enrolled := kiosk.json(http.MethodPost, "/api/v1/kiosk/enroll", map[string]string{"token": fmt.Sprint(device["token"])}, http.StatusOK)
	kiosk.csrf = fmt.Sprint(enrolled["csrfToken"])
	response := kiosk.do(http.MethodPost, "/api/v1/lobby/walk-ins", visitBody(siteID, nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("walk-in without a host returned %d: %s", response.Code, response.Body.String())
	}
}

func TestKioskCheckInRecordsDeviceLobby(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	reference := env.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK)
	lobbies, _ := reference["lobbies"].([]any)
	lobbyID := fmt.Sprint(lobbies[0].(map[string]any)["id"])
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, nil), http.StatusCreated)
	token := passTokenFrom(t, created)
	device := env.json(http.MethodPost, "/api/v1/admin/kiosk-devices", map[string]any{"name": "kiosk", "siteId": siteID, "lobbyId": lobbyID, "validDays": 1}, http.StatusCreated)
	kiosk := &testEnv{server: env.server, handler: env.handler, t: t}
	enrolled := kiosk.json(http.MethodPost, "/api/v1/kiosk/enroll", map[string]string{"token": fmt.Sprint(device["token"])}, http.StatusOK)
	kiosk.csrf = fmt.Sprint(enrolled["csrfToken"])
	kiosk.json(http.MethodPost, "/api/v1/checkins", map[string]string{"token": token, "method": "kiosk"}, http.StatusCreated)
	var recorded string
	if err := env.server.db.QueryRow(context.Background(), `SELECT COALESCE(lobby_id,'') FROM visit_events WHERE event_type='CHECKED_IN' ORDER BY created_at DESC LIMIT 1`).Scan(&recorded); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if recorded != lobbyID {
		t.Fatalf("check-in event recorded lobby %q, want the kiosk's lobby %q", recorded, lobbyID)
	}
}

func TestCancelRevokesQRAndPendingNotifications(t *testing.T) {
	env := newTestEnv(t)
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(env.siteID(), nil), http.StatusCreated)
	visitID := fmt.Sprint(created["id"])
	token := passTokenFrom(t, created)
	env.json(http.MethodPost, "/api/v1/visits/"+visitID+"/cancel", map[string]any{}, http.StatusNoContent)
	if verify := env.do(http.MethodPost, "/api/v1/qr/verify", map[string]string{"token": token}); verify.Code != http.StatusConflict {
		t.Fatalf("QR of a cancelled visit still verifies: %d %s", verify.Code, verify.Body.String())
	}
	var pending int
	if err := env.server.db.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE visit_id=$1 AND status IN ('queued','failed','sending') AND rule_id NOT IN (SELECT id FROM notification_rules WHERE event='visit_cancelled')`, visitID).Scan(&pending); err != nil {
		t.Fatalf("count: %v", err)
	}
	if pending != 0 {
		t.Fatalf("%d pre-visit notifications remain queued after cancellation", pending)
	}
	if again := env.do(http.MethodPost, "/api/v1/visits/"+visitID+"/cancel", map[string]any{}); again.Code != http.StatusConflict {
		t.Fatalf("cancelling twice returned %d", again.Code)
	}
}

func TestAdminMasterDataValidation(t *testing.T) {
	env := newTestEnv(t)
	// Duplicate site code is a conflict, not a server error.
	if dup := env.do(http.MethodPost, "/api/v1/admin/sites", map[string]any{"code": "HQ", "name": "중복 본사"}); dup.Code != http.StatusConflict {
		t.Fatalf("duplicate site code returned %d: %s", dup.Code, dup.Body.String())
	}
	if bad := env.do(http.MethodPost, "/api/v1/admin/sites", map[string]any{"code": "LAB", "name": "연구소", "timezone": "Mars/Olympus"}); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid timezone returned %d", bad.Code)
	}
	site := env.json(http.MethodPost, "/api/v1/admin/sites", map[string]any{"code": "LAB", "name": "연구소", "timezone": "Asia/Tokyo"}, http.StatusOK)
	var timezone string
	if err := env.server.db.QueryRow(context.Background(), `SELECT timezone FROM sites WHERE id=$1`, fmt.Sprint(site["id"])).Scan(&timezone); err != nil || timezone != "Asia/Tokyo" {
		t.Fatalf("site timezone not stored: %q %v", timezone, err)
	}
	// Disabling a watch list entry that does not exist is a 404, not a 500.
	if missing := env.do(http.MethodDelete, "/api/v1/admin/watchlist/does-not-exist", nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing watch list entry returned %d", missing.Code)
	}
	if self := env.do(http.MethodPost, "/api/v1/admin/organizations", map[string]any{"id": "org-1", "name": "순환", "parentId": "org-1"}); self.Code != http.StatusBadRequest {
		t.Fatalf("self-parented organization returned %d", self.Code)
	}
}

func TestOnlySuperAdminManagesSuperAdmins(t *testing.T) {
	env := newTestEnv(t)
	adminID := env.createLocalUser("ops-admin", RoleAdmin, "")
	targetID := env.createLocalUser("someone", RoleUser, "")
	ops := &testEnv{server: env.server, handler: env.handler, t: t}
	ops.login("ops-admin", testAdminPassword)
	if escalate := ops.do(http.MethodPatch, "/api/v1/admin/users/"+targetID, map[string]any{"role": RoleSuperAdmin}); escalate.Code != http.StatusForbidden {
		t.Fatalf("an admin granted super_admin: %d", escalate.Code)
	}
	if selfEscalate := ops.do(http.MethodPatch, "/api/v1/admin/users/"+adminID, map[string]any{"role": RoleSuperAdmin}); selfEscalate.Code != http.StatusForbidden {
		t.Fatalf("an admin promoted themselves: %d", selfEscalate.Code)
	}
	ops.json(http.MethodPatch, "/api/v1/admin/users/"+targetID, map[string]any{"role": RoleLobby, "siteScope": []string{env.siteID()}}, http.StatusNoContent)
	env.json(http.MethodPatch, "/api/v1/admin/users/"+targetID, map[string]any{"role": RoleSuperAdmin}, http.StatusNoContent)
}

func TestAuditLogPagingAndVisitorSearch(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	for index := 0; index < 3; index++ {
		env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, map[string]any{"visitors": []map[string]any{{"name": fmt.Sprintf("검색%d", index), "phone": fmt.Sprintf("010-9999-000%d", index), "company": "찾기상사", "consent": true}}}), http.StatusCreated)
	}
	first := env.json(http.MethodGet, "/api/v1/admin/audit-logs?limit=2", nil, http.StatusOK)
	if first["hasMore"] != true {
		t.Fatalf("audit paging did not report more rows: %v", first)
	}
	second := env.json(http.MethodGet, fmt.Sprintf("/api/v1/admin/audit-logs?limit=2&before=%v", first["nextBefore"]), nil, http.StatusOK)
	firstItems, _ := first["items"].([]any)
	secondItems, _ := second["items"].([]any)
	if len(secondItems) == 0 || fmt.Sprint(secondItems[0].(map[string]any)["id"]) == fmt.Sprint(firstItems[0].(map[string]any)["id"]) {
		t.Fatalf("audit paging repeated or returned nothing: %v", second)
	}
	filtered := env.json(http.MethodGet, "/api/v1/admin/audit-logs?action=visit.create", nil, http.StatusOK)
	for _, item := range filtered["items"].([]any) {
		if fmt.Sprint(item.(map[string]any)["action"]) != "visit.create" {
			t.Fatalf("action filter leaked %v", item)
		}
	}
	byName := env.json(http.MethodGet, "/api/v1/admin/visitors?q="+url.QueryEscape("검색1"), nil, http.StatusOK)
	if items, _ := byName["items"].([]any); len(items) != 1 {
		t.Fatalf("visitor search by name returned %d rows", len(items))
	}
	byPhone := env.json(http.MethodGet, "/api/v1/admin/visitors?q="+url.QueryEscape("010-9999-0002"), nil, http.StatusOK)
	if items, _ := byPhone["items"].([]any); len(items) != 1 {
		t.Fatalf("visitor search by phone returned %d rows", len(items))
	}
	byCompany := env.json(http.MethodGet, "/api/v1/admin/visitors?q="+url.QueryEscape("찾기"), nil, http.StatusOK)
	if items, _ := byCompany["items"].([]any); len(items) != 3 {
		t.Fatalf("visitor search by company returned %d rows", len(items))
	}
}

func TestProfileUpdateKeepsPhoneUnlessProvided(t *testing.T) {
	env := newTestEnv(t)
	env.json(http.MethodPatch, "/api/v1/profile", map[string]any{"displayName": "관리자", "phone": "010-2222-3333"}, http.StatusNoContent)
	me := env.json(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if me["phoneMasked"] != "010-****-3333" {
		t.Fatalf("phone not stored or not masked: %v", me["phoneMasked"])
	}
	// A rename that omits the phone must not delete it.
	env.json(http.MethodPatch, "/api/v1/profile", map[string]any{"displayName": "관리자2"}, http.StatusNoContent)
	if me := env.json(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK); me["phoneMasked"] != "010-****-3333" {
		t.Fatalf("renaming wiped the phone: %v", me["phoneMasked"])
	}
	if bad := env.do(http.MethodPatch, "/api/v1/profile", map[string]any{"displayName": "관리자2", "phone": "12"}); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid phone accepted: %d", bad.Code)
	}
	env.json(http.MethodPatch, "/api/v1/profile", map[string]any{"displayName": "관리자2", "phone": ""}, http.StatusNoContent)
	if me := env.json(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK); me["phoneMasked"] != "" {
		t.Fatalf("explicit empty phone did not clear it: %v", me["phoneMasked"])
	}
}

func TestLobbyMustBelongToSiteAndVerifyHonoursReuseSetting(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	other := env.json(http.MethodPost, "/api/v1/admin/sites", map[string]any{"code": "LAB", "name": "연구소"}, http.StatusOK)
	lobby := env.json(http.MethodPost, "/api/v1/admin/lobbies", map[string]any{"siteId": fmt.Sprint(other["id"]), "code": "L1", "name": "연구소 로비"}, http.StatusOK)
	if mismatch := env.do(http.MethodPost, "/api/v1/visits", visitBody(siteID, map[string]any{"lobbyId": fmt.Sprint(lobby["id"])})); mismatch.Code != http.StatusBadRequest {
		t.Fatalf("lobby from another site accepted: %d %s", mismatch.Code, mismatch.Body.String())
	}
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, nil), http.StatusCreated)
	token := passTokenFrom(t, created)
	env.json(http.MethodPost, "/api/v1/checkins", map[string]string{"token": token}, http.StatusCreated)
	if reused := env.do(http.MethodPost, "/api/v1/qr/verify", map[string]string{"token": token}); reused.Code != http.StatusConflict {
		t.Fatalf("used QR verified with single-use on: %d", reused.Code)
	}
	// With single-use off the scanner and the check-in must agree that reuse is fine.
	if _, err := env.server.db.Exec(context.Background(), `UPDATE settings SET value='false' WHERE key='visit.single_use_qr'`); err != nil {
		t.Fatalf("configure: %v", err)
	}
	env.json(http.MethodPost, "/api/v1/qr/verify", map[string]string{"token": token}, http.StatusOK)
}

func TestParticipantCancelAndSeriesCancel(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, map[string]any{"visitors": []map[string]any{
		{"name": "첫째", "phone": "010-1000-0001", "consent": true},
		{"name": "둘째", "phone": "010-1000-0002", "consent": true},
	}}), http.StatusCreated)
	visitID := fmt.Sprint(created["id"])
	detail := env.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	visitors := detail["visitors"].([]any)
	second := fmt.Sprint(visitors[1].(map[string]any)["id"])
	result := env.json(http.MethodPost, "/api/v1/visitor-visits/"+second+"/cancel", map[string]any{}, http.StatusOK)
	if result["visitCancelled"] != false || fmt.Sprint(result["remaining"]) != "1" {
		t.Fatalf("cancelling one of two visitors gave %v", result)
	}
	after := env.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	if visit := after["visit"].(map[string]any); visit["status"] != "SCHEDULED" {
		t.Fatalf("visit status changed after a single participant cancel: %v", visit["status"])
	}
	first := fmt.Sprint(visitors[0].(map[string]any)["id"])
	last := env.json(http.MethodPost, "/api/v1/visitor-visits/"+first+"/cancel", map[string]any{}, http.StatusOK)
	if last["visitCancelled"] != true {
		t.Fatalf("cancelling the last visitor did not cancel the visit: %v", last)
	}

	series := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, map[string]any{"recurrence": map[string]any{"frequency": "weekly", "occurrences": 3}}), http.StatusCreated)
	occurrences := series["occurrences"].([]any)
	middle := fmt.Sprint(occurrences[1].(map[string]any)["id"])
	cancelled := env.json(http.MethodPost, "/api/v1/visits/"+middle+"/cancel-series", map[string]any{}, http.StatusOK)
	if fmt.Sprint(cancelled["cancelled"]) != "2" {
		t.Fatalf("series cancel from the second occurrence cancelled %v visits, want 2", cancelled["cancelled"])
	}
	firstOccurrence := env.json(http.MethodGet, "/api/v1/visits/"+fmt.Sprint(occurrences[0].(map[string]any)["id"]), nil, http.StatusOK)
	if visit := firstOccurrence["visit"].(map[string]any); visit["status"] != "SCHEDULED" {
		t.Fatalf("an earlier occurrence was cancelled: %v", visit["status"])
	}
}

func TestManualCheckInAndRejectionDetail(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, nil), http.StatusCreated)
	visitID := fmt.Sprint(created["id"])
	token := passTokenFrom(t, created)
	detail := env.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	participantID := fmt.Sprint(detail["visitors"].([]any)[0].(map[string]any)["id"])
	if missing := env.do(http.MethodPost, "/api/v1/checkins/manual", map[string]any{"visitorVisitId": participantID}); missing.Code != http.StatusBadRequest {
		t.Fatalf("manual check-in without a reason returned %d", missing.Code)
	}
	env.json(http.MethodPost, "/api/v1/checkins/manual", map[string]any{"visitorVisitId": participantID, "reason": "신분증 확인, 휴대폰 방전"}, http.StatusCreated)
	// The QR is consumed so it cannot be replayed after a manual admission.
	if replay := env.do(http.MethodPost, "/api/v1/checkins", map[string]string{"token": token}); replay.Code != http.StatusConflict {
		t.Fatalf("QR still usable after manual check-in: %d", replay.Code)
	}
	after := env.json(http.MethodGet, "/api/v1/visits/"+visitID, nil, http.StatusOK)
	events := after["detail"].(map[string]any)["events"].([]any)
	found := false
	for _, event := range events {
		entry := event.(map[string]any)
		if entry["type"] == "CHECKED_IN" && entry["method"] == "manual" {
			found = true
		}
	}
	if !found {
		t.Fatalf("timeline lacks the manual check-in: %v", events)
	}

	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"visit.approval_enabled": "true"}}, http.StatusOK)
	pending := env.json(http.MethodPost, "/api/v1/visits", visitBody(siteID, nil), http.StatusCreated)
	pendingID := fmt.Sprint(pending["id"])
	env.json(http.MethodPost, "/api/v1/visits/"+pendingID+"/reject", map[string]string{"reason": "일정 중복"}, http.StatusNoContent)
	rejected := env.json(http.MethodGet, "/api/v1/visits/"+pendingID, nil, http.StatusOK)
	if extras := rejected["detail"].(map[string]any); extras["approvalReason"] != "일정 중복" || extras["approverName"] == "" {
		t.Fatalf("rejection reason or approver missing from detail: %v", extras)
	}
	if summary := rejected["visit"].(map[string]any); summary["approvalReason"] != "일정 중복" {
		t.Fatalf("summary lacks the rejection reason: %v", summary)
	}
}

func TestFailedNotificationCanBeRetried(t *testing.T) {
	env := newTestEnv(t)
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(env.siteID(), nil), http.StatusCreated)
	visitID := fmt.Sprint(created["id"])
	ctx := context.Background()
	recipient, err := env.server.keys.Encrypt("01012345678")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	body, err := env.server.keys.Encrypt("실패한 알림")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	id := newID()
	if _, err := env.server.db.Exec(ctx, `INSERT INTO notifications(id,visit_id,recipient_encrypted,channel,template_key,body_encrypted,status,attempts,error)
		VALUES($1,$2,$3,'sms','test',$4,'failed',5,'provider down')`, id, visitID, recipient, body); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	result := env.json(http.MethodPost, "/api/v1/admin/notifications/"+id+"/retry", map[string]any{}, http.StatusOK)
	if fmt.Sprint(result["queued"]) != "1" {
		t.Fatalf("retry did not requeue the notification: %v", result)
	}
	var status string
	var attempts int
	if err := env.server.db.QueryRow(ctx, `SELECT status,attempts FROM notifications WHERE id=$1`, id).Scan(&status, &attempts); err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if status != "queued" || attempts != 0 {
		t.Fatalf("notification is %s with %d attempts after retry", status, attempts)
	}
	if conflict := env.do(http.MethodPost, "/api/v1/admin/notifications/"+id+"/retry", map[string]any{}); conflict.Code != http.StatusConflict {
		t.Fatalf("retrying a queued notification returned %d", conflict.Code)
	}
}

func TestPublicEndpointsAreRateLimited(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.server.db.Exec(context.Background(), `UPDATE settings SET value='2' WHERE key='security.public_rate_limit_per_minute'`); err != nil {
		t.Fatalf("configure limit: %v", err)
	}
	// The limit is cached for a few seconds; expire it so the new value applies.
	env.server.limitCacheMu.Lock()
	env.server.limitCacheExpires = time.Time{}
	env.server.limitCacheMu.Unlock()
	public := &testEnv{server: env.server, handler: env.handler, t: t}
	for attempt := 0; attempt < 2; attempt++ {
		if response := public.do(http.MethodGet, "/api/v1/public/passes/vfq_missing", nil); response.Code != http.StatusNotFound {
			t.Fatalf("attempt %d returned %d, want 404", attempt+1, response.Code)
		}
	}
	limited := public.do(http.MethodGet, "/api/v1/public/passes/vfq_missing", nil)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("public endpoint was not rate limited: %d", limited.Code)
	}
}

func TestAuditLogExportProducesCSV(t *testing.T) {
	env := newTestEnv(t)
	env.json(http.MethodPost, "/api/v1/visits", visitBody(env.siteID(), nil), http.StatusCreated)
	response := env.do(http.MethodGet, "/api/v1/admin/audit-logs.csv", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export returned %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Fatalf("unexpected content type %q", got)
	}
	body := response.Body.String()
	if !strings.HasPrefix(body, "\ufeff") {
		t.Fatal("CSV export is missing the UTF-8 BOM Excel needs")
	}
	if !strings.Contains(body, "visit.create") {
		t.Fatalf("export does not contain the recorded action: %s", body[:minInt(len(body), 400)])
	}
}

func TestMetricsEndpointRequiresConfiguredToken(t *testing.T) {
	env := newTestEnv(t)
	if disabled := env.do(http.MethodGet, "/metrics", nil); disabled.Code != http.StatusNotFound {
		t.Fatalf("metrics endpoint is exposed before configuration: %d", disabled.Code)
	}
	encrypted, err := env.server.keys.Encrypt("scrape-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := env.server.db.Exec(context.Background(), `UPDATE settings SET value=$1 WHERE key='security.metrics_token'`, encrypted); err != nil {
		t.Fatalf("configure token: %v", err)
	}
	if unauthorized := env.do(http.MethodGet, "/metrics", nil); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("metrics endpoint accepted an unauthenticated scrape: %d", unauthorized.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer scrape-token")
	response := httptest.NewRecorder()
	env.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized scrape returned %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "visitflow_schema_version") {
		t.Fatalf("metrics output is missing gauges: %s", response.Body.String())
	}
}

// The lobby dashboard streams updates over SSE, which only works if every
// middleware wrapping the ResponseWriter still forwards Flush.
func TestLobbyStreamFlushesEvents(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/lobby/stream", nil).WithContext(ctx)
	request.RemoteAddr = "10.0.0.1:5000"
	for _, cookie := range env.cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	env.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stream returned %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("unexpected content type %q", got)
	}
	if !strings.Contains(response.Body.String(), "event: ready") {
		t.Fatalf("stream did not flush the ready event: %q", response.Body.String())
	}
}

func TestReadyzReportsSchemaAndBacklog(t *testing.T) {
	env := newTestEnv(t)
	body := env.json(http.MethodGet, "/readyz", nil, http.StatusOK)
	if body["status"] != "ready" {
		t.Fatalf("readyz reported %v", body["status"])
	}
	if fmt.Sprint(body["schemaVersion"]) != fmt.Sprint(database.ExpectedSchemaVersion()) {
		t.Fatalf("readyz schema version %v does not match %d", body["schemaVersion"], database.ExpectedSchemaVersion())
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
