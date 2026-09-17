package app

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// MCP over SSO. The authorization flow — PKCE, the redirect, the code exchange
// — belongs to Keycloak and the client. What is this server's is the resource
// server half: it says where the authorization server is, turns a 401 into a
// pointer there, and accepts exactly the tokens that server issued for this
// resource, for a person VisitFlow already knows, with the powers a key would
// have and no more. These tests hold it to that with a fake Keycloak that
// signs real RS256 tokens and serves discovery and JWKS.

type fakeIDP struct {
	key    *rsa.PrivateKey
	server *httptest.Server
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": idp.server.URL, "authorization_endpoint": idp.server.URL + "/auth", "token_endpoint": idp.server.URL + "/token",
			"jwks_uri": idp.server.URL + "/certs", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/certs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func jwtSegment(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// sign issues a token the way the realm key would: RS256 with the advertised kid.
func (idp *fakeIDP) sign(t *testing.T, payload map[string]any) string {
	t.Helper()
	signing := jwtSegment(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"}) + "." + jwtSegment(payload)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// accessToken is what Keycloak hands an MCP client after the person signed in:
// issued by the realm, for an audience, with the subject bound to an account.
func (idp *fakeIDP) accessToken(subject string, audience any, extra map[string]any) map[string]any {
	payload := map[string]any{
		"iss": idp.server.URL, "aud": audience, "sub": subject, "typ": "Bearer",
		"exp": time.Now().Add(5 * time.Minute).Unix(), "iat": time.Now().Unix(),
		"preferred_username": "sso-" + subject, "scope": "openid profile email",
	}
	for k, v := range extra {
		payload[k] = v
	}
	return payload
}

// enableMCPOAuth configures SSO the way the settings screen does, then turns
// the MCP switch on. The public base URL makes the resource identifier
// deterministic regardless of the test request's host.
func (e *testEnv) enableMCPOAuth(t *testing.T, idp *fakeIDP, extra map[string]string) {
	t.Helper()
	settings := map[string]string{
		"oidc.enabled": "true", "oidc.issuer_url": idp.server.URL, "oidc.client_id": "visitflow-web", "oidc.client_secret": "test-secret",
		"general.base_url": "https://visit.example.test", "mcp.oauth.enabled": "true",
	}
	for k, v := range extra {
		settings[k] = v
	}
	e.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": settings}, http.StatusOK)
}

// createSSOUser registers the account a web login would have created and bound.
func (e *testEnv) createSSOUser(username, issuer, subject, role string, active bool) string {
	e.t.Helper()
	id := newID()
	if _, err := e.server.db.Exec(e.t.Context(), `INSERT INTO users(id,username,display_name,role,source,oidc_issuer,oidc_subject,active) VALUES($1,$2,$2,$3,'oidc',$4,NULLIF($5,''),$6)`,
		id, username, role, issuer, subject, active); err != nil {
		e.t.Fatalf("create sso user %s: %v", username, err)
	}
	return id
}

const mcpListTools = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

// mcpWith posts one MCP request with the given bearer, without any session.
func (e *testEnv) mcpWith(bearer, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	ctx, cancel := e.requestContext()
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body)).WithContext(ctx)
	request.RemoteAddr = "10.0.0.1:5000"
	request.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	return response
}

func mcpCallBody(tool string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	return string(b)
}

func (e *testEnv) userCount() int {
	e.t.Helper()
	var n int
	if err := e.server.db.QueryRow(e.t.Context(), `SELECT count(*) FROM users`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestMCPOAuthIsOffByDefaultAndTellsARefusedClientWhereToSignIn(t *testing.T) {
	env := newTestEnv(t)
	idp := newFakeIDP(t)

	// A fresh installation advertises nothing, and a token — however real its
	// signature — is refused exactly as any stray bearer was before.
	for _, path := range []string{mcpMetadataPath, mcpMetadataPath + mcpPath} {
		if response := env.do(http.MethodGet, path, nil); response.Code != http.StatusNotFound {
			t.Fatalf("%s served with SSO off: %d %s", path, response.Code, response.Body.String())
		}
	}
	bare := env.mcpWith("", mcpListTools)
	if bare.Code != http.StatusUnauthorized || bare.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("a 401 with SSO off pointed somewhere: %d %q", bare.Code, bare.Header().Get("WWW-Authenticate"))
	}
	token := idp.sign(t, idp.accessToken("subject-1", "https://visit.example.test/mcp", nil))
	off := env.mcpWith(token, mcpListTools)
	if off.Code != http.StatusUnauthorized || !strings.Contains(off.Body.String(), "authentication_required") || off.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("a token with SSO off was not refused like any bearer: %d %s %q", off.Code, off.Body.String(), off.Header().Get("WWW-Authenticate"))
	}

	// Turning it on without an issuer is refused at save time.
	incomplete := env.do(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"mcp.oauth.enabled": "true"}})
	if incomplete.Code != http.StatusBadRequest || !strings.Contains(incomplete.Body.String(), "mcp_oauth_incomplete") {
		t.Fatalf("enabling without an issuer was accepted: %d %s", incomplete.Code, incomplete.Body.String())
	}
	env.enableMCPOAuth(t, idp, nil)

	// RFC 9728: a bare document naming the resource and its authorization
	// server, readable cross-origin, on both well-known paths.
	for _, path := range []string{mcpMetadataPath, mcpMetadataPath + mcpPath} {
		response := env.mcpWithoutSession(http.MethodGet, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s is not readable by a browser-hosted client", path)
		}
		var metadata struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			BearerMethods        []string `json:"bearer_methods_supported"`
			Scopes               []string `json:"scopes_supported"`
			Error                any      `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil || metadata.Error != nil {
			t.Fatalf("%s is not a bare metadata document: %s", path, response.Body.String())
		}
		if metadata.Resource != "https://visit.example.test/mcp" {
			t.Errorf("resource %q, want the public base URL plus /mcp", metadata.Resource)
		}
		if len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != idp.server.URL {
			t.Errorf("authorization_servers %v, want the configured issuer", metadata.AuthorizationServers)
		}
		if strings.Join(metadata.BearerMethods, ",") != "header" || strings.Join(metadata.Scopes, " ") != "read mcp" {
			t.Errorf("bearer_methods %v scopes %v", metadata.BearerMethods, metadata.Scopes)
		}
	}

	// The 401 now carries the pointer; a refused token adds error=invalid_token.
	want := `resource_metadata="https://visit.example.test/.well-known/oauth-protected-resource/mcp"`
	refused := env.mcpWith("", mcpListTools)
	header := refused.Header().Get("WWW-Authenticate")
	if refused.Code != http.StatusUnauthorized || !strings.HasPrefix(header, `Bearer realm="VisitFlow"`) || !strings.Contains(header, want) || strings.Contains(header, "invalid_token") {
		t.Fatalf("bare 401: %d %q", refused.Code, header)
	}
	garbage := env.mcpWith("not.a.jwt", mcpListTools)
	if header := garbage.Header().Get("WWW-Authenticate"); garbage.Code != http.StatusUnauthorized || !strings.Contains(header, want) || !strings.Contains(header, `error="invalid_token"`) {
		t.Fatalf("refused token 401: %d %q", garbage.Code, header)
	}
	badKey := env.mcpWith("vf_not_a_real_key", mcpListTools)
	if header := badKey.Header().Get("WWW-Authenticate"); badKey.Code != http.StatusUnauthorized || !strings.Contains(header, want) {
		t.Fatalf("bad key 401 on /mcp: %d %q", badKey.Code, header)
	}
	// REST 401s stay exactly as they were: a browser must not be sent to Keycloak.
	rest := env.mcpWithoutSession(http.MethodGet, "/api/v1/auth/me")
	if rest.Code != http.StatusUnauthorized || rest.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("REST 401 carries the MCP challenge: %d %q", rest.Code, rest.Header().Get("WWW-Authenticate"))
	}

	// A configured resource identifier wins over the base URL and must be the
	// MCP endpoint itself.
	if response := env.do(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"mcp.oauth.resource": "https://mcp.example.test/"}}); response.Code != http.StatusBadRequest {
		t.Fatalf("a resource without /mcp was accepted: %d %s", response.Code, response.Body.String())
	}
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"mcp.oauth.resource": "https://mcp.example.test/mcp"}}, http.StatusOK)
	var metadata struct {
		Resource string `json:"resource"`
	}
	_ = json.Unmarshal(env.mcpWithoutSession(http.MethodGet, mcpMetadataPath+mcpPath).Body.Bytes(), &metadata)
	if metadata.Resource != "https://mcp.example.test/mcp" {
		t.Fatalf("configured resource ignored: %q", metadata.Resource)
	}
	if header := env.mcpWith("", mcpListTools).Header().Get("WWW-Authenticate"); !strings.Contains(header, `resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("challenge does not follow the configured resource: %q", header)
	}
	policy := env.json(http.MethodGet, "/api/v1/api-key-policy", nil, http.StatusOK)
	info, _ := policy["mcpOAuth"].(map[string]any)
	if info["enabled"] != true || info["resource"] != "https://mcp.example.test/mcp" || info["metadataUrl"] != "https://mcp.example.test/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("the key page is not told how to connect over SSO: %v", policy["mcpOAuth"])
	}
}

// mcpWithoutSession sends a request carrying no cookie and no bearer.
func (e *testEnv) mcpWithoutSession(method, path string) *httptest.ResponseRecorder {
	e.t.Helper()
	ctx, cancel := e.requestContext()
	defer cancel()
	request := httptest.NewRequest(method, path, nil).WithContext(ctx)
	request.RemoteAddr = "10.0.0.1:5000"
	response := httptest.NewRecorder()
	e.handler.ServeHTTP(response, request)
	return response
}

func TestMCPOAuthTokenOpensMCPForARegisteredAccountOnly(t *testing.T) {
	env := newTestEnv(t)
	idp := newFakeIDP(t)
	env.enableMCPOAuth(t, idp, nil)
	resource := "https://visit.example.test/mcp"
	env.createSSOUser("member", idp.server.URL, "subject-member", RoleUser, true)
	env.createSSOUser("suspended", idp.server.URL, "subject-suspended", RoleUser, false)
	// An account an administrator created for SSO that has not signed in yet
	// has no subject bound; the web login matches it by username, so does this.
	env.createSSOUser("unbound", idp.server.URL, "", RoleUser, true)
	users := env.userCount()

	// The canonical path: an Audience mapper put the resource in aud.
	opened := env.mcpWith(idp.sign(t, idp.accessToken("subject-member", resource, nil)), mcpListTools)
	if opened.Code != http.StatusOK || !strings.Contains(opened.Body.String(), "search_visits") {
		t.Fatalf("a token for this resource was refused: %d %s", opened.Code, opened.Body.String())
	}
	// A real Keycloak 26 puts `account` in aud and the client in azp. The web
	// client is this application's own, so its tokens are accepted as-is…
	viaWebClient := env.mcpWith(idp.sign(t, idp.accessToken("subject-member", "account", map[string]any{"azp": "visitflow-web"})), mcpListTools)
	if viaWebClient.Code != http.StatusOK {
		t.Fatalf("a token issued to the web client was refused: %d %s", viaWebClient.Code, viaWebClient.Body.String())
	}
	// …and a token issued to some other application in the realm is not ours,
	// however real its signature. The refusal says what was seen and what to
	// enter, so the operator can finish the setup from that one message.
	other := env.mcpWith(idp.sign(t, idp.accessToken("subject-member", "account", map[string]any{"azp": "claude-mcp"})), mcpListTools)
	if other.Code != http.StatusUnauthorized {
		t.Fatalf("a token for another application opened MCP: %d %s", other.Code, other.Body.String())
	}
	var refusal struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(other.Body.Bytes(), &refusal)
	for _, want := range []string{`aud=[account]`, `azp="claude-mcp"`, `mcp.oauth.audience`, `"claude-mcp"`, `"` + resource + `"`} {
		if !strings.Contains(refusal.Error.Message, want) {
			t.Errorf("the audience refusal does not say %s: %s", want, refusal.Error.Message)
		}
	}
	// The administrator lists that client id instead of adding a mapper.
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"mcp.oauth.audience": "claude-mcp cursor-mcp"}}, http.StatusOK)
	listed := env.mcpWith(idp.sign(t, idp.accessToken("subject-member", "account", map[string]any{"azp": "claude-mcp"})), mcpListTools)
	if listed.Code != http.StatusOK {
		t.Fatalf("a listed azp was refused: %d %s", listed.Code, listed.Body.String())
	}
	// A subject Keycloak knows and VisitFlow does not, or one that was
	// deactivated, is refused — and nothing is created or revived.
	stranger := env.mcpWith(idp.sign(t, idp.accessToken("subject-stranger", resource, nil)), mcpListTools)
	if stranger.Code != http.StatusUnauthorized || !strings.Contains(stranger.Body.String(), "account_not_registered") || !strings.Contains(stranger.Body.String(), "웹으로 한 번 로그인") {
		t.Fatalf("unknown subject: %d %s", stranger.Code, stranger.Body.String())
	}
	if suspended := env.mcpWith(idp.sign(t, idp.accessToken("subject-suspended", resource, nil)), mcpListTools); suspended.Code != http.StatusUnauthorized {
		t.Fatalf("a deactivated account came back to life over MCP: %d %s", suspended.Code, suspended.Body.String())
	}
	if env.userCount() != users {
		t.Fatalf("a token created an account: %d users, had %d", env.userCount(), users)
	}
	unbound := env.mcpWith(idp.sign(t, idp.accessToken("subject-unbound", resource, map[string]any{"preferred_username": "unbound"})), mcpListTools)
	if unbound.Code != http.StatusOK {
		t.Fatalf("an SSO account awaiting its first login was refused: %d %s", unbound.Code, unbound.Body.String())
	}
	// A local account with the same username is a different identity.
	env.createLocalUser("localonly", RoleUser, "")
	if local := env.mcpWith(idp.sign(t, idp.accessToken("subject-local", resource, map[string]any{"preferred_username": "localonly"})), mcpListTools); local.Code != http.StatusUnauthorized {
		t.Fatalf("a token matched a local account by name: %d %s", local.Code, local.Body.String())
	}
	// The token's role claims mean nothing: the account's own role applies.
	elevated := env.mcpWith(idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"realm_access": map[string]any{"roles": []string{"admin"}}, "groups": []string{"/visitflow-admins"}})), mcpCallBody("get_visit_statistics", nil))
	if elevated.Code != http.StatusOK || !strings.Contains(elevated.Body.String(), "관리자 권한이 필요합니다") {
		t.Fatalf("a role claim elevated an SSO subject: %d %s", elevated.Code, elevated.Body.String())
	}
	// OAuth tokens open /mcp only. REST keeps refusing them as before.
	ctx, cancel := env.requestContext()
	defer cancel()
	rest := httptest.NewRequest(http.MethodGet, "/api/v1/visits", nil).WithContext(ctx)
	rest.RemoteAddr = "10.0.0.1:5000"
	rest.Header.Set("Authorization", "Bearer "+idp.sign(t, idp.accessToken("subject-member", resource, nil)))
	restResponse := httptest.NewRecorder()
	env.handler.ServeHTTP(restResponse, rest)
	if restResponse.Code != http.StatusUnauthorized || !strings.Contains(restResponse.Body.String(), "authentication_required") {
		t.Fatalf("an SSO token opened a REST path: %d %s", restResponse.Code, restResponse.Body.String())
	}
}

func TestMCPOAuthScopesAreTheAdministratorsCeilingNarrowedByTheToken(t *testing.T) {
	env := newTestEnv(t)
	idp := newFakeIDP(t)
	env.enableMCPOAuth(t, idp, nil)
	resource := "https://visit.example.test/mcp"
	env.createSSOUser("member", idp.server.URL, "subject-member", RoleUser, true)
	siteID := env.siteID()
	start := time.Now().Add(2 * time.Hour).UTC()
	createArgs := map[string]any{"site_id": siteID, "start_at": start.Format(time.RFC3339), "end_at": start.Add(time.Hour).Format(time.RFC3339), "purpose": "SSO 방문", "visitor_name": "홍길동", "visitor_phone": "010-1234-5678", "consent": true}
	tokenWithScope := func(scope string) string {
		return idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"scope": scope}))
	}

	// Default ceiling "read mcp": a token that only speaks Keycloak's own
	// scope words gets exactly that — reads work, writes do not.
	if response := env.mcpWith(tokenWithScope("openid profile email"), mcpCallBody("create_visit", createArgs)); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "write 범위가 필요합니다") {
		t.Fatalf("default ceiling allowed a write: %d %s", response.Code, response.Body.String())
	}
	// A wider ceiling lets a plain token write…
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"mcp.oauth.scopes": "read write mcp"}}, http.StatusOK)
	if response := env.mcpWith(tokenWithScope("openid"), mcpCallBody("create_visit", createArgs)); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"isError":false`) {
		t.Fatalf("ceiling with write refused a write: %d %s", response.Code, response.Body.String())
	}
	// …but a token that carries this app's vocabulary without write is held
	// to the intersection, and one carrying none of the ceiling's scopes is
	// refused outright rather than handed an empty (= unrestricted) list.
	if response := env.mcpWith(tokenWithScope("openid read mcp"), mcpCallBody("create_visit", createArgs)); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "write 범위가 필요합니다") {
		t.Fatalf("token scope without write was not intersected: %d %s", response.Code, response.Body.String())
	}
	if response := env.mcpWith(tokenWithScope("openid read"), mcpListTools); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "insufficient_scope") {
		t.Fatalf("token scope without mcp was not refused: %d %s", response.Code, response.Body.String())
	}
	if response := env.mcpWith(tokenWithScope("openid write"), mcpCallBody("create_visit", createArgs)); response.Code != http.StatusForbidden {
		t.Fatalf("token scope lacking mcp opened a write tool: %d %s", response.Code, response.Body.String())
	}
	// The key policy is the same door: what the administrator removed from
	// keys is removed from SSO subjects too.
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"security.api_key_allowed_scopes": "read mcp"}}, http.StatusOK)
	if response := env.mcpWith(tokenWithScope("openid"), mcpCallBody("create_visit", createArgs)); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "write 범위가 필요합니다") {
		t.Fatalf("key policy did not cap SSO scopes: %d %s", response.Code, response.Body.String())
	}
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"security.api_key_allowed_scopes": "read write"}}, http.StatusOK)
	if response := env.mcpWith(tokenWithScope("openid"), mcpListTools); response.Code != http.StatusForbidden {
		t.Fatalf("mcp removed from the key policy still opened MCP over SSO: %d %s", response.Code, response.Body.String())
	}
	// Saving a ceiling without mcp is refused: it could never open MCP.
	if response := env.do(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"mcp.oauth.scopes": "read"}}); response.Code != http.StatusBadRequest {
		t.Fatalf("a ceiling without mcp was accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestMCPOAuthRefusesBadTokensAndLogsWhichCheckFailed(t *testing.T) {
	env := newTestEnv(t)
	idp := newFakeIDP(t)
	env.enableMCPOAuth(t, idp, nil)
	resource := "https://visit.example.test/mcp"
	env.createSSOUser("member", idp.server.URL, "subject-member", RoleUser, true)
	var logs bytes.Buffer
	env.server.logger = slog.New(slog.NewTextHandler(&logs, nil))

	refused := func(what, bearer, wantCause string) {
		t.Helper()
		logs.Reset()
		response := env.mcpWith(bearer, mcpListTools)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d %s", what, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Errorf("%s: no invalid_token challenge: %q", what, response.Header().Get("WWW-Authenticate"))
		}
		// The client is told to sign in again; the log keeps the real reason.
		if !strings.Contains(logs.String(), "mcp oauth token refused") || !strings.Contains(logs.String(), wantCause) {
			t.Errorf("%s: the log does not name the failed check %q: %s", what, wantCause, logs.String())
		}
	}
	refused("expired", idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})), "expired")
	refused("not yet valid", idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"nbf": time.Now().Add(time.Hour).Unix()})), "nbf")
	refused("other issuer", idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"iss": "https://other-realm.example.test"})), "different provider")
	foreign := newFakeIDP(t)
	refused("foreign signature", foreign.sign(t, idp.accessToken("subject-member", resource, nil)), "signature")
	refused("id token", idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"typ": "ID"})), "typ=ID")
	refused("bound token", idp.sign(t, idp.accessToken("subject-member", resource, map[string]any{"cnf": map[string]string{"jkt": "x"}})), "cnf")
	refused("no subject", idp.sign(t, idp.accessToken("", resource, nil)), "sub")
	// A symmetric signature over the realm's public key is the classic
	// confusion attack; the algorithm allow-list stops it before any key lookup.
	signing := jwtSegment(map[string]string{"alg": "HS256", "typ": "JWT", "kid": "test"}) + "." + jwtSegment(idp.accessToken("subject-member", resource, nil))
	mac := hmac.New(sha256.New, idp.key.PublicKey.N.Bytes())
	mac.Write([]byte(signing))
	refused("hs256", signing+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), "verify")
	unsigned := jwtSegment(map[string]string{"alg": "none", "typ": "JWT"}) + "." + jwtSegment(idp.accessToken("subject-member", resource, nil)) + ".x"
	refused("alg none", unsigned, "verify")

	// And the good token still opens after all of that: nothing above poisoned
	// the provider cache.
	if response := env.mcpWith(idp.sign(t, idp.accessToken("subject-member", resource, nil)), mcpListTools); response.Code != http.StatusOK {
		t.Fatalf("a valid token was refused after the bad ones: %d %s", response.Code, response.Body.String())
	}
	// A key keeps working exactly as before, side by side.
	if stats := env.mcpTool("get_lobby_status", nil); stats == nil {
		t.Fatal("the personal key path broke")
	}
}

func TestMCPOAuthRefusalWhenKeycloakIsUnreachable(t *testing.T) {
	env := newTestEnv(t)
	idp := newFakeIDP(t)
	env.enableMCPOAuth(t, idp, nil)
	env.createSSOUser("member", idp.server.URL, "subject-member", RoleUser, true)
	token := idp.sign(t, idp.accessToken("subject-member", "https://visit.example.test/mcp", nil))
	idp.server.Close()
	response := env.mcpWith(token, mcpListTools)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "oidc_discovery_failed") {
		t.Fatalf("unreachable issuer: %d %s", response.Code, response.Body.String())
	}
	// The failure is remembered briefly, so a second token does not wait on
	// another connection attempt.
	started := time.Now()
	if again := env.mcpWith(token, mcpListTools); again.Code != http.StatusUnauthorized {
		t.Fatalf("second attempt: %d", again.Code)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("second attempt retried discovery: %s", time.Since(started))
	}
	// The metadata document is served regardless — it is what points a
	// client to the issuer in the first place, and it needs no round trip.
	if metadata := env.mcpWithoutSession(http.MethodGet, mcpMetadataPath+mcpPath); metadata.Code != http.StatusOK {
		t.Fatalf("metadata while the issuer is down: %d", metadata.Code)
	}
}

func TestMCPOAuthHelpers(t *testing.T) {
	for token, want := range map[string]bool{"a.b.c": true, "vf_abc": false, "a.b": false, ".b.c": false, "a..c": false, "a.b.": false, "a.b.c.d": false} {
		if looksLikeJWT(token) != want {
			t.Errorf("looksLikeJWT(%q) = %v", token, !want)
		}
	}
	if got := mcpMetadataURL("https://visit.example.test/mcp"); got != "https://visit.example.test/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("metadata url %q", got)
	}
	cases := []struct {
		ceiling      []string
		allowed      string
		tokenScope   string
		want         string
		wantDeclined bool
	}{
		{[]string{"read", "mcp"}, "read write mcp", "openid profile email", "read mcp", false},
		{[]string{"read", "write", "mcp"}, "read write mcp", "openid read mcp", "read mcp", false},
		{[]string{"read", "write", "mcp"}, "read write mcp", "openid read", "read", true},
		{[]string{"read", "write", "mcp"}, "read mcp", "", "read mcp", false},
		{[]string{"read", "write", "mcp"}, "read write", "openid", "read write", true},
		{[]string{"read", "mcp", "bogus", "mcp"}, "read write mcp", "", "read mcp", false},
		{[]string{"mcp"}, "read write mcp", "write", "", true},
	}
	for _, c := range cases {
		got := mcpOAuthEffectiveScopes(c.ceiling, c.allowed, c.tokenScope)
		if strings.Join(got, " ") != c.want {
			t.Errorf("effective(%v, %q, %q) = %v, want %q", c.ceiling, c.allowed, c.tokenScope, got, c.want)
		}
		if declined := !containsString(got, "mcp"); declined != c.wantDeclined {
			t.Errorf("effective(%v, %q, %q) declined=%v, want %v", c.ceiling, c.allowed, c.tokenScope, declined, c.wantDeclined)
		}
	}
	for _, c := range [][2]string{
		{"mcp.oauth.enabled", "yes"}, {"mcp.oauth.resource", "https://visit.example.test"}, {"mcp.oauth.resource", "https://visit.example.test/mcp?x=1"},
		{"mcp.oauth.resource", "ftp://visit.example.test/mcp"}, {"mcp.oauth.scopes", "read"}, {"mcp.oauth.scopes", "mcp mcp"}, {"mcp.oauth.scopes", "mcp admin"},
	} {
		if validateSettingValue(c[0], c[1]) == "" {
			t.Errorf("%s=%q was accepted", c[0], c[1])
		}
	}
	for key, value := range map[string]string{
		"mcp.oauth.enabled": "false", "mcp.oauth.resource": "https://visit.example.test/mcp", "mcp.oauth.scopes": "read mcp", "mcp.oauth.audience": "claude-mcp https://other.example.test/mcp",
	} {
		if message := validateSettingValue(key, value); message != "" {
			t.Errorf("%s=%q was refused: %s", key, value, message)
		}
	}
	if validateSettingValue("mcp.oauth.scopes", "") == "" {
		t.Error("an empty scope ceiling was accepted; it could never open MCP")
	}
}
