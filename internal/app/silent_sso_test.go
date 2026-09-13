package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// fakeOIDCIssuer serves just enough of an OpenID discovery document for the
// start handler to build an authorization URL. No token exchange happens in
// these tests: what is under test is which address the browser is sent to and
// how a provider refusal is answered.
func fakeOIDCIssuer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": server.URL, "authorization_endpoint": server.URL + "/auth", "token_endpoint": server.URL + "/token",
			"jwks_uri": server.URL + "/certs", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func (e *testEnv) enableOIDC(t *testing.T, issuer string) {
	t.Helper()
	e.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{
		"oidc.enabled": "true", "oidc.issuer_url": issuer, "oidc.client_id": "visitflow", "oidc.client_secret": "test-secret",
	}}, http.StatusOK)
}

// startOIDC follows the start redirect and returns the authorization URL the
// browser would be sent to.
func (e *testEnv) startOIDC(t *testing.T, query string) *url.URL {
	t.Helper()
	response := e.do(http.MethodGet, "/api/v1/auth/oidc/start"+query, nil)
	if response.Code != http.StatusFound {
		t.Fatalf("oidc start returned %d: %s", response.Code, response.Body.String())
	}
	target, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	return target
}

func (e *testEnv) oidcStateRow(t *testing.T, state string) (silent bool, returnTo string) {
	t.Helper()
	if err := e.server.db.QueryRow(context.Background(), `SELECT silent,return_to FROM oidc_states WHERE state_hash=$1`, e.server.keys.Digest(state)).Scan(&silent, &returnTo); err != nil {
		t.Fatalf("state row: %v", err)
	}
	return silent, returnTo
}

func TestSilentSSORequiresAutoLogin(t *testing.T) {
	env := newTestEnv(t)
	issuer := fakeOIDCIssuer(t)
	env.enableOIDC(t, issuer.URL)

	config := env.json(http.MethodGet, "/api/v1/auth/config", nil, http.StatusOK)
	if config["oidcEnabled"] != true || config["oidcAutoLogin"] != false {
		t.Fatalf("auto-login must be off by default: %v", config)
	}
	// With the setting off, ?prompt=none on the address is quietly downgraded
	// to an ordinary login: nobody can change the flow by editing the URL.
	target := env.startOIDC(t, "?prompt=none&returnTo=%2Fvisits")
	if target.Query().Get("prompt") != "" {
		t.Fatalf("prompt=none was forwarded although auto-login is off: %s", target)
	}
	if target.Query().Get("state") == "" || target.Query().Get("code_challenge") == "" {
		t.Fatalf("ordinary authorization request is incomplete: %s", target)
	}
	if silent, returnTo := env.oidcStateRow(t, target.Query().Get("state")); silent || returnTo != "/visits" {
		t.Fatalf("state row silent=%v return_to=%q, want an ordinary state bound for /visits", silent, returnTo)
	}
	// A refusal of an ordinary attempt keeps the existing error landing.
	callback := env.do(http.MethodGet, "/api/v1/auth/oidc/callback?error=login_required&state="+url.QueryEscape(target.Query().Get("state")), nil)
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != "/login?error=login_required" {
		t.Fatalf("ordinary refusal returned %d %q", callback.Code, callback.Header().Get("Location"))
	}
}

func TestSilentSSORefusalLandsOnMarkedLogin(t *testing.T) {
	env := newTestEnv(t)
	issuer := fakeOIDCIssuer(t)
	env.enableOIDC(t, issuer.URL)
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"oidc.auto_login": "true"}}, http.StatusOK)

	config := env.json(http.MethodGet, "/api/v1/auth/config", nil, http.StatusOK)
	if config["oidcAutoLogin"] != true {
		t.Fatalf("auth config does not publish auto-login: %v", config)
	}
	// The SSO button still starts an ordinary login; only an explicit
	// prompt=none becomes a silent attempt.
	if plain := env.startOIDC(t, "?returnTo=%2Fvisits"); plain.Query().Get("prompt") != "" {
		t.Fatalf("plain start forwarded prompt: %s", plain)
	}
	target := env.startOIDC(t, "?prompt=none&returnTo=%2Fadmin%2Fvisits%3Fstatus%3DPENDING")
	if target.Query().Get("prompt") != "none" {
		t.Fatalf("silent start did not ask for prompt=none: %s", target)
	}
	state := target.Query().Get("state")
	if silent, returnTo := env.oidcStateRow(t, state); !silent || returnTo != "/admin/visits?status=PENDING" {
		t.Fatalf("state row silent=%v return_to=%q, want a silent state bound for the deep link", silent, returnTo)
	}
	// No session at the provider: login_required is an ordinary answer. The
	// browser lands on the login screen with the marker that stops a retry.
	callback := env.do(http.MethodGet, "/api/v1/auth/oidc/callback?error=login_required&error_description=x&state="+url.QueryEscape(state), nil)
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != "/login?sso=none" {
		t.Fatalf("silent refusal returned %d %q, want /login?sso=none", callback.Code, callback.Header().Get("Location"))
	}
	// The state is single-use: replaying the refusal is no longer silent.
	replay := env.do(http.MethodGet, "/api/v1/auth/oidc/callback?error=login_required&state="+url.QueryEscape(state), nil)
	if replay.Header().Get("Location") != "/login?error=login_required" {
		t.Fatalf("replayed refusal returned %q", replay.Header().Get("Location"))
	}
	// A refusal with no state at all cannot claim to be silent either.
	if bare := env.do(http.MethodGet, "/api/v1/auth/oidc/callback?error=login_required", nil); bare.Header().Get("Location") != "/login?error=login_required" {
		t.Fatalf("stateless refusal returned %q", bare.Header().Get("Location"))
	}
	// Only same-origin paths are carried through the provider and back.
	if _, returnTo := env.oidcStateRow(t, env.startOIDC(t, "?prompt=none&returnTo=%2F%2Fevil.example%2F").Query().Get("state")); returnTo != "/" {
		t.Fatalf("protocol-relative return_to was stored: %q", returnTo)
	}
}

func TestSilentSSOSettingValidation(t *testing.T) {
	if message := validateSettingValue("oidc.auto_login", "yes"); message == "" {
		t.Fatal("non-boolean auto_login accepted")
	}
	if message := validateSettingValue("oidc.auto_login", "true"); message != "" {
		t.Fatalf("boolean auto_login rejected: %s", message)
	}
}

func TestOIDCReturnTo(t *testing.T) {
	cases := map[string]string{
		"/": "/", "/visits": "/visits", "/admin/visits?status=PENDING#top": "/admin/visits?status=PENDING#top",
		"": "/", "visits": "/", "//evil.example/": "/", "https://evil.example/": "/", "/\r\nLocation: x": "/",
		"/\\evil.example": "/",
	}
	for raw, want := range cases {
		if got := oidcReturnTo(raw); got != want {
			t.Errorf("oidcReturnTo(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestOIDCSilentRequested(t *testing.T) {
	silent := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start?prompt=none", nil)
	plain := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	other := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start?prompt=login", nil)
	if !oidcSilentRequested(silent, true) {
		t.Fatal("prompt=none with auto-login on must be silent")
	}
	if oidcSilentRequested(silent, false) || oidcSilentRequested(plain, true) || oidcSilentRequested(other, true) {
		t.Fatal("silent attempt without auto-login or without prompt=none")
	}
}
