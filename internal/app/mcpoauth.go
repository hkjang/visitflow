package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
)

// MCP over SSO — /mcp opened with a Keycloak access token instead of a key.
//
// The MCP authorization specification (2025-06-18 and later) is OAuth 2.1: the
// MCP server is a resource server that publishes where its authorization server
// is (RFC 9728), refuses a bare request with a 401 that points there, and
// accepts the access tokens that server issued for this resource. Keycloak does
// the login and mints the token; this file only decides whether a presented
// token is one Keycloak issued for this server, and for whom.
//
// The personal key stays and is unchanged. A token from SSO is a second way
// through the same door: it must belong to an account that already exists and
// is active, it receives the scopes the administrator set (never more than a
// key could have), and it is accepted on /mcp only. It never creates an account
// and never reads a role out of the token.

const (
	mcpPath                = "/mcp"
	mcpMetadataPath        = "/.well-known/oauth-protected-resource"
	mcpOAuthDiscoveryLimit = 10 * time.Second
	// mcpOAuthDiscoveryRetry is how long a failed discovery is remembered, so a
	// Keycloak outage costs one round trip every few seconds rather than one
	// per presented token.
	mcpOAuthDiscoveryRetry = 5 * time.Second
)

// mcpScopeVocabulary is the app's own scope vocabulary, shared with keys.
var mcpScopeVocabulary = []string{"read", "write", "mcp"}

// mcpOAuthSigningAlgs are the only algorithms a token may be signed with.
// Symmetric HS* and "none" are excluded: a key set can only verify asymmetric
// signatures, and anything else is a forgery attempt.
var mcpOAuthSigningAlgs = []string{oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512}

type mcpOAuthConfig struct {
	Enabled bool
	// Issuer is the authorization server, shared with the web sign-in.
	Issuer string
	// ClientID is the web sign-in's client. A token Keycloak issued to it is
	// for this application by definition, so it is accepted as an audience.
	ClientID string
	// Resource is the configured identifier; empty means derive it from the
	// public base URL (or, last of all, from the request).
	Resource string
	// Audiences are further accepted aud/azp values the administrator named.
	Audiences []string
	// Scopes is the ceiling an SSO subject receives. Keycloak does not know
	// this app's vocabulary unless somebody teaches it, so the administrator
	// states the ceiling here; a token that does carry the vocabulary only
	// narrows it.
	Scopes []string
}

func (s *Server) mcpOAuthConfig(ctx context.Context) mcpOAuthConfig {
	enabled, _ := s.getSetting(ctx, "mcp.oauth.enabled")
	issuer, _ := s.getSetting(ctx, "oidc.issuer_url")
	clientID, _ := s.getSetting(ctx, "oidc.client_id")
	resource, _ := s.getSetting(ctx, "mcp.oauth.resource")
	audience, _ := s.getSetting(ctx, "mcp.oauth.audience")
	scopes, _ := s.getSetting(ctx, "mcp.oauth.scopes")
	cfg := mcpOAuthConfig{
		Enabled:   enabled == "true",
		Issuer:    strings.TrimSpace(issuer),
		ClientID:  strings.TrimSpace(clientID),
		Resource:  strings.TrimRight(strings.TrimSpace(resource), "/"),
		Audiences: strings.Fields(audience),
		Scopes:    strings.Fields(scopes),
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"read", "mcp"}
	}
	if cfg.Enabled && cfg.Issuer == "" {
		// Saving refuses this combination, but an imported or hand-edited
		// settings table can still hold it; behave as if off and say why.
		s.logger.Warn("mcp oauth is enabled but oidc.issuer_url is empty; SSO tokens are not accepted")
	}
	return cfg
}

// active reports whether SSO tokens are accepted at all: the switch is on and
// the authorization server is known.
func (c mcpOAuthConfig) active() bool { return c.Enabled && c.Issuer != "" }

// mcpResource is the identifier this deployment claims for its MCP endpoint —
// what the metadata advertises and what a token's aud must name. It comes from
// mcp.oauth.resource, else the public base URL setting; the request's own host
// is the last resort, since anybody can set that header.
func (s *Server) mcpResource(ctx context.Context, r *http.Request, cfg mcpOAuthConfig) string {
	if cfg.Resource != "" {
		return cfg.Resource
	}
	return s.publicBaseURL(ctx, r) + mcpPath
}

// mcpMetadataURL is where a refused client is sent to learn the above.
func mcpMetadataURL(resource string) string {
	return strings.TrimSuffix(resource, mcpPath) + mcpMetadataPath + mcpPath
}

// looksLikeJWT is the cheap shape test that separates a key from a token, so
// a bearer that is neither gets exactly the refusal it always got.
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// mcpOAuthProvider caches discovery per issuer. Discovery is a round trip to
// Keycloak and the key set behind it verifies every token; doing that per
// request would put Keycloak's latency in front of every MCP call. go-oidc
// refetches the key set on an unknown key id, so rotation needs no cache
// invalidation here, and a changed issuer setting simply misses the cache.
func (s *Server) mcpOAuthProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	if provider := s.oauthProviders[issuer]; provider != nil {
		return provider, nil
	}
	if s.oauthFailure != nil && issuer == s.oauthFailure.issuer && time.Now().Before(s.oauthFailure.until) {
		return nil, s.oauthFailure.err
	}
	// The provider keeps this context for later key fetches, so it must
	// outlive the request that created it, and it must carry a client with
	// a deadline so a stalled Keycloak cannot hold an MCP request forever.
	client := &http.Client{Timeout: mcpOAuthDiscoveryLimit}
	provider, err := oidc.NewProvider(oidc.ClientContext(context.WithoutCancel(ctx), client), issuer)
	if err != nil {
		s.oauthFailure = &oauthDiscoveryFailure{issuer: issuer, err: err, until: time.Now().Add(mcpOAuthDiscoveryRetry)}
		return nil, err
	}
	if s.oauthProviders == nil {
		s.oauthProviders = map[string]*oidc.Provider{}
	}
	s.oauthProviders[issuer] = provider
	s.oauthFailure = nil
	return provider, nil
}

type oauthDiscoveryFailure struct {
	issuer string
	err    error
	until  time.Time
}

// mcpOAuthRefusal says why a token is not accepted: the message goes to the
// client, the cause — which check failed — goes to the log.
type mcpOAuthRefusal struct {
	status  int
	code    string
	message string
	cause   error
}

func (r *mcpOAuthRefusal) Error() string { return r.cause.Error() }

func refuseToken(code, message string, cause error) *mcpOAuthRefusal {
	return &mcpOAuthRefusal{status: http.StatusUnauthorized, code: code, message: message, cause: cause}
}

// mcpOAuthPrincipal turns a bearer access token into the user it stands for
// and the scopes that user receives, or says exactly why it will not.
func (s *Server) mcpOAuthPrincipal(ctx context.Context, r *http.Request, cfg mcpOAuthConfig, token string) (User, []string, *mcpOAuthRefusal) {
	provider, err := s.mcpOAuthProvider(ctx, cfg.Issuer)
	if err != nil {
		return User{}, nil, refuseToken("oidc_discovery_failed",
			"Keycloak 발급자 정보를 읽지 못해 SSO 토큰을 확인할 수 없습니다. 잠시 후 다시 시도하거나 관리자에게 알리세요", fmt.Errorf("discovery: %w", err))
	}
	// Signature, issuer, expiry and nbf. The audience is checked below by
	// hand because more than one value is acceptable and azp counts too.
	verified, err := provider.Verifier(&oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: mcpOAuthSigningAlgs}).Verify(ctx, token)
	if err != nil {
		return User{}, nil, refuseToken("invalid_token",
			"SSO 액세스 토큰이 유효하지 않습니다(서명·발급자·만료). 클라이언트에서 다시 로그인하세요", fmt.Errorf("verify: %w", err))
	}
	var claims struct {
		Type              string          `json:"typ"`
		AuthorizedParty   string          `json:"azp"`
		Scope             string          `json:"scope"`
		PreferredUsername string          `json:"preferred_username"`
		Email             string          `json:"email"`
		Confirmation      json.RawMessage `json:"cnf"`
	}
	if err := verified.Claims(&claims); err != nil {
		return User{}, nil, refuseToken("invalid_token", "SSO 토큰의 내용을 읽을 수 없습니다", fmt.Errorf("claims: %w", err))
	}
	// An ID token proves a login happened; it is not an API credential and
	// Keycloak marks it typ=ID. A token bound to a key (cnf) needs a proof
	// this server cannot check, so it is refused rather than half-accepted.
	if strings.EqualFold(claims.Type, "ID") {
		return User{}, nil, refuseToken("invalid_token",
			"ID 토큰은 MCP 자격이 아닙니다. 액세스 토큰을 보내세요", errors.New("typ=ID"))
	}
	if len(claims.Confirmation) > 0 && string(claims.Confirmation) != "null" {
		return User{}, nil, refuseToken("invalid_token",
			"소지자 증명(cnf)이 묶인 토큰은 이 서버가 검증할 수 없습니다", errors.New("cnf present"))
	}
	if strings.TrimSpace(verified.Subject) == "" {
		return User{}, nil, refuseToken("invalid_token", "SSO 토큰에 사용자 식별자(sub)가 없습니다", errors.New("sub empty"))
	}
	// Whom the token was minted for. A real Keycloak 26 puts `account` in aud
	// and the client id in azp, so both are compared against the resource,
	// the administrator's list and the web client. Anything else is a token
	// for some other application in the realm, which is exactly what RFC
	// 8707 exists to keep out.
	resource := s.mcpResource(ctx, r, cfg)
	accepted := append([]string{resource}, cfg.Audiences...)
	if cfg.ClientID != "" {
		accepted = append(accepted, cfg.ClientID)
	}
	bound := append(slices.Clone(verified.Audience), claims.AuthorizedParty)
	if !slices.ContainsFunc(bound, func(v string) bool { return v != "" && slices.Contains(accepted, v) }) {
		return User{}, nil, refuseToken("invalid_token",
			fmt.Sprintf("SSO 토큰이 이 서버를 위해 발급된 것이 아닙니다(aud=%v, azp=%q). 관리자가 허용 대상(mcp.oauth.audience)에 %q를 적거나, Keycloak 클라이언트에 Audience 매퍼로 %q를 더해야 합니다",
				verified.Audience, claims.AuthorizedParty, claims.AuthorizedParty, resource),
			fmt.Errorf("audience %v / azp %q not accepted", verified.Audience, claims.AuthorizedParty))
	}
	// The same lookup the web sign-in uses, minus the provisioning half: the
	// subject bound at a previous web login, or an account an administrator
	// created for SSO that has not been bound yet. Nothing is written.
	username := strings.TrimSpace(claims.PreferredUsername)
	if username == "" {
		username = strings.TrimSpace(claims.Email)
	}
	var u User
	err = s.db.QueryRow(ctx, `SELECT u.id,u.username,u.display_name,COALESCE(u.email,''),u.employee_id,u.role,u.source,u.last_login_at,
		u.department_id,u.site_scope,u.delegate_user_id,u.delegate_until,
		EXISTS(SELECT 1 FROM users m WHERE m.delegate_user_id=u.id AND m.delegate_until>now() AND m.active AND m.role='dept_manager')
		FROM users u WHERE u.active=true AND ((u.oidc_issuer=$1 AND u.oidc_subject=$2)
			OR (u.source='oidc' AND u.oidc_subject IS NULL AND $3<>'' AND lower(u.username)=lower($3)))
		ORDER BY (u.oidc_subject=$2) DESC NULLS LAST LIMIT 1`, cfg.Issuer, verified.Subject, username).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.EmployeeID, &u.Role, &u.Source, &u.LastLoginAt,
			&u.DepartmentID, &u.SiteScope, &u.DelegateUserID, &u.DelegateUntil, &u.ApprovalDelegate)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, refuseToken("account_not_registered",
			"이 SSO 계정은 VisitFlow에 등록되지 않았거나 비활성입니다. 먼저 웹으로 한 번 로그인하세요", errors.New("no active account for subject"))
	}
	if err != nil {
		return User{}, nil, &mcpOAuthRefusal{status: http.StatusInternalServerError, code: "database_error", message: "데이터를 처리하지 못했습니다", cause: err}
	}
	// The scopes are the administrator's ceiling, held to the same policy a
	// key is, narrowed by the token if it speaks this app's vocabulary. An
	// empty result is refused here: downstream an empty scope list means
	// "unrestricted", which is the opposite of what an empty intersection means.
	allowedValue, _ := s.getSetting(ctx, "security.api_key_allowed_scopes")
	scopes := mcpOAuthEffectiveScopes(cfg.Scopes, allowedValue, claims.Scope)
	if !containsString(scopes, "mcp") {
		return User{}, nil, &mcpOAuthRefusal{status: http.StatusForbidden, code: "insufficient_scope",
			message: fmt.Sprintf("SSO 토큰 주체에게 허용된 범위 %v에 mcp가 없습니다. 관리자 설정 mcp.oauth.scopes와 허용 키 범위, 토큰의 scope를 확인하세요", scopes),
			cause:   fmt.Errorf("effective scopes %v lack mcp (ceiling %v, token scope %q)", scopes, cfg.Scopes, claims.Scope)}
	}
	return u, scopes, nil
}

// mcpOAuthEffectiveScopes intersects the administrator's ceiling with the key
// policy and, when the token carries any of this app's scope words, with the
// token. A token whose scope is only Keycloak's own words (openid, profile,
// email) leaves the ceiling as it is.
func mcpOAuthEffectiveScopes(ceiling []string, allowedValue, tokenScope string) []string {
	allowed := filterAllowedScopes(ceiling, allowedValue)
	tokenScopes := strings.Fields(tokenScope)
	if !slices.ContainsFunc(tokenScopes, func(v string) bool { return slices.Contains(mcpScopeVocabulary, v) }) {
		return allowed
	}
	out := []string{}
	for _, scope := range allowed {
		if slices.Contains(tokenScopes, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// filterAllowedScopes keeps the scopes the administrator's key policy
// (security.api_key_allowed_scopes) allows, in order and without repeats.
func filterAllowedScopes(scopes []string, allowedValue string) []string {
	allowed := map[string]bool{}
	for _, scope := range strings.Fields(allowedValue) {
		allowed[scope] = true
	}
	out := []string{}
	for _, scope := range scopes {
		if allowed[scope] && slices.Contains(mcpScopeVocabulary, scope) && !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// mcpChallenge turns a 401 on /mcp into an invitation: the client reads
// resource_metadata and starts the OAuth flow from there. It is set on the MCP
// path only — a REST 401 carrying it would send browsers somewhere wrong.
func (s *Server) mcpChallenge(w http.ResponseWriter, r *http.Request, tokenPresented bool) {
	if r.URL.Path != mcpPath {
		return
	}
	cfg := s.mcpOAuthConfig(r.Context())
	if !cfg.active() {
		return
	}
	header := fmt.Sprintf(`Bearer realm="VisitFlow", resource_metadata=%q`, mcpMetadataURL(s.mcpResource(r.Context(), r, cfg)))
	if tokenPresented {
		header += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", header)
}

// protectedResourceMetadata is RFC 9728: the document a refused MCP client
// reads to find the authorization server. Public by design — it says where to
// sign in, not who is signed in — and a bare document rather than this API's
// error envelope, because the reader is an OAuth library.
func (s *Server) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	// Browser-hosted MCP clients read this cross-origin.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	cfg := s.mcpOAuthConfig(r.Context())
	if !cfg.active() {
		writeError(w, http.StatusNotFound, "mcp_oauth_disabled", "이 서버의 MCP는 SSO 토큰을 받지 않습니다. 개인 API 키(vf_)를 사용하세요")
		return
	}
	serviceName, _ := s.getSetting(r.Context(), "general.service_name")
	if serviceName == "" {
		serviceName = "VisitFlow"
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.mcpResource(r.Context(), r, cfg),
		"authorization_servers":    []string{cfg.Issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         cfg.Scopes,
		"resource_name":            serviceName + " MCP",
	})
}

// mcpOAuthInfo is what the key page and the settings screen show a person so
// they can connect with nothing but the URL: whether SSO is on and where the
// endpoint and its metadata live.
func (s *Server) mcpOAuthInfo(ctx context.Context, r *http.Request) map[string]any {
	cfg := s.mcpOAuthConfig(ctx)
	resource := s.mcpResource(ctx, r, cfg)
	return map[string]any{"enabled": cfg.active(), "resource": resource, "metadataUrl": mcpMetadataURL(resource), "scopes": cfg.Scopes}
}
