package app

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const sessionCookie = "visitflow_session"

func (s *Server) EnsureBootstrapAdmin(ctx context.Context, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	email := ""
	if strings.Contains(username, "@") {
		email = username
	}
	_, err = s.db.Exec(ctx, `INSERT INTO users(id, username, password_hash, display_name, email, role, source)
		VALUES($1,$2,$3,$2,NULLIF($4,''),'super_admin','local') ON CONFLICT (username) DO NOTHING`, newID(), username, string(hash), email)
	return err
}

func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	serviceName, _ := s.getSetting(r.Context(), "general.service_name")
	companyName, _ := s.getSetting(r.Context(), "general.company_name")
	local, _ := s.getSetting(r.Context(), "auth.local_enabled")
	oidcEnabled, _ := s.getSetting(r.Context(), "oidc.enabled")
	writeJSON(w, http.StatusOK, map[string]any{
		"serviceName": serviceName, "companyName": companyName,
		"localEnabled": local == "true", "oidcEnabled": oidcEnabled == "true",
		"version": map[string]string{"version": s.version, "commit": s.commit, "builtAt": s.builtAt},
	})
}

func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	local, _ := s.getSetting(r.Context(), "auth.local_enabled")
	if local != "true" {
		writeError(w, http.StatusForbidden, "local_login_disabled", "로컬 로그인이 비활성화되어 있습니다")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	username := strings.TrimSpace(in.Username)
	throttleKeys := loginThrottleKeys(clientIP(r), username)
	if remaining := s.loginLock(r.Context(), throttleKeys); remaining > 0 {
		s.metrics.loginLockouts.Add(1)
		s.audit(r.Context(), "", "auth.login_locked", "user", "", r.RemoteAddr, map[string]string{"username": username})
		w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "login_locked",
			fmt.Sprintf("로그인 시도가 많아 %d분 동안 잠겼습니다", int(remaining.Minutes())+1))
		return
	}
	var hash string
	u, err := scanUser(s.db.QueryRow(r.Context(), `SELECT id,username,display_name,COALESCE(email,''),employee_id,role,source,last_login_at FROM users WHERE lower(username)=lower($1) AND active=true`, username))
	if err == nil {
		err = s.db.QueryRow(r.Context(), `SELECT COALESCE(password_hash,'') FROM users WHERE id=$1`, u.ID).Scan(&hash)
	}
	if err != nil || hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		s.metrics.loginFailures.Add(1)
		s.recordLoginFailure(r.Context(), throttleKeys)
		s.audit(r.Context(), "", "auth.login_failed", "user", "", r.RemoteAddr, map[string]string{"source": "local", "username": username})
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "아이디 또는 비밀번호를 확인하세요")
		return
	}
	if err := s.issueSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, "session_error", "로그인 세션을 만들지 못했습니다")
		return
	}
	s.clearLoginFailures(r.Context(), throttleKeys)
	s.audit(r.Context(), u.ID, "auth.login", "user", u.ID, r.RemoteAddr, map[string]string{"source": "local"})
	writeJSON(w, http.StatusOK, u)
}

// loginThrottleKeys counts a failure against both the source address and the
// account, so neither password spraying across accounts nor a targeted attack
// from many addresses slips past a single counter.
func loginThrottleKeys(ip, username string) []string {
	keys := []string{"ip:" + ip}
	if username != "" {
		keys = append(keys, "user:"+strings.ToLower(username))
	}
	return keys
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, u User) error {
	raw, err := platform.RandomToken(32)
	if err != nil {
		return err
	}
	csrf, err := platform.RandomToken(24)
	if err != nil {
		return err
	}
	hours := 8
	if v, _ := s.getSetting(r.Context(), "security.session_hours"); v != "" {
		if parsed, e := strconv.Atoi(v); e == nil && parsed >= 1 && parsed <= 720 {
			hours = parsed
		}
	}
	expires := time.Now().Add(time.Duration(hours) * time.Hour)
	if _, err := s.db.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,ip_address,user_agent) VALUES($1,$2,$3,$4,$5,$6)`, s.keys.Digest(raw), u.ID, csrf, expires, r.RemoteAddr, r.UserAgent()); err != nil {
		return err
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE users SET last_login_at=now() WHERE id=$1`, u.ID)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: raw, Path: "/", HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
	return nil
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return s.authenticated(next, false)
}

// authenticateLobby additionally accepts an enrolled kiosk device cookie. Only
// the lobby route group uses it, so a device token can never reach personal,
// admin or audit endpoints.
func (s *Server) authenticateLobby(next http.Handler) http.Handler {
	return s.authenticated(next, true)
}

func (s *Server) authenticated(next http.Handler, allowKiosk bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u User
		var csrf string
		var apiScopes []string
		apiKeyAuth := false
		if allowKiosk {
			if device, found := s.kioskFromRequest(r); found {
				if !kioskCSRFValid(r) {
					writeError(w, http.StatusForbidden, "csrf_failed", "키오스크 보안 토큰이 유효하지 않습니다")
					return
				}
				ctx := context.WithValue(r.Context(), userContextKey, device.user())
				ctx = context.WithValue(ctx, kioskContextKey, device)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			raw := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			if strings.HasPrefix(raw, "vf_") {
				var keyID string
				var err error
				u, err = scanUser(s.db.QueryRow(r.Context(), `SELECT u.id,u.username,u.display_name,COALESCE(u.email,''),u.employee_id,u.role,u.source,u.last_login_at
					FROM api_keys k JOIN users u ON u.id=k.user_id
					WHERE k.secret_hash=$1 AND u.active=true AND (k.expires_at IS NULL OR k.expires_at>now()) AND (k.revoked_at IS NULL OR k.grace_until>now())`, s.keys.Digest(raw)))
				if err == nil {
					err = s.db.QueryRow(r.Context(), `SELECT id,scopes FROM api_keys WHERE secret_hash=$1`, s.keys.Digest(raw)).Scan(&keyID, &apiScopes)
					_, _ = s.db.Exec(r.Context(), `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, keyID)
				}
				if err != nil {
					writeError(w, http.StatusUnauthorized, "invalid_api_key", "API 키가 유효하지 않습니다")
					return
				}
				allowedValue, _ := s.getSetting(r.Context(), "security.api_key_allowed_scopes")
				allowed := map[string]bool{}
				for _, scope := range strings.Fields(allowedValue) {
					allowed[scope] = true
				}
				filtered := apiScopes[:0]
				for _, scope := range apiScopes {
					if allowed[scope] {
						filtered = append(filtered, scope)
					}
				}
				apiScopes = filtered
				apiKeyAuth = true
				readRequest := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
				if r.URL.Path != "/mcp" && readRequest && !containsString(apiScopes, "read") {
					writeError(w, http.StatusForbidden, "insufficient_scope", "read 범위가 있는 API 키가 필요합니다")
					return
				}
				if r.URL.Path != "/mcp" && !readRequest && !containsString(apiScopes, "write") {
					writeError(w, http.StatusForbidden, "insufficient_scope", "write 범위가 있는 API 키가 필요합니다")
					return
				}
			}
		}
		if !apiKeyAuth {
			cookie, err := r.Cookie(sessionCookie)
			if err != nil || cookie.Value == "" {
				writeError(w, http.StatusUnauthorized, "authentication_required", "로그인이 필요합니다")
				return
			}
			u, err = scanUser(s.db.QueryRow(r.Context(), `SELECT u.id,u.username,u.display_name,COALESCE(u.email,''),u.employee_id,u.role,u.source,u.last_login_at
				FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active=true`, s.keys.Digest(cookie.Value)))
			if err != nil {
				http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
				writeError(w, http.StatusUnauthorized, "session_expired", "세션이 만료되었습니다")
				return
			}
			_ = s.db.QueryRow(r.Context(), `SELECT csrf_token FROM sessions WHERE token_hash=$1`, s.keys.Digest(cookie.Value)).Scan(&csrf)
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && r.URL.Path != "/api/v1/auth/logout" {
				provided := r.Header.Get("X-CSRF-Token")
				if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(csrf)) != 1 {
					writeError(w, http.StatusForbidden, "csrf_failed", "보안 토큰이 유효하지 않습니다")
					return
				}
			}
		}
		s.loadUserScope(r.Context(), &u)
		ctx := context.WithValue(r.Context(), userContextKey, u)
		ctx = context.WithValue(ctx, csrfContextKey, csrf)
		ctx = context.WithValue(ctx, apiScopesContextKey, apiScopes)
		ctx = context.WithValue(ctx, apiKeyAuthContextKey, apiKeyAuth)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r)
		if !u.IsAdmin() {
			writeError(w, http.StatusForbidden, "admin_required", "시스템 관리자 권한이 필요합니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireLobby(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r)
		if !u.CanManageLobby() {
			writeError(w, http.StatusForbidden, "lobby_required", "로비 담당자 권한이 필요합니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAudit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r)
		if !u.CanAudit() {
			writeError(w, http.StatusForbidden, "auditor_required", "감사 조회 권한이 필요합니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r)
		if u.Role != RoleSecurity && !u.IsAdmin() {
			writeError(w, http.StatusForbidden, "security_required", "보안 담당자 권한이 필요합니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loadUserScope(ctx context.Context, u *User) {
	if u == nil || u.ID == "" {
		return
	}
	_ = s.db.QueryRow(ctx, `SELECT department_id,site_scope,delegate_user_id,delegate_until,
		EXISTS(SELECT 1 FROM users m WHERE m.delegate_user_id=users.id AND m.delegate_until>now() AND m.active AND m.role='dept_manager')
		FROM users WHERE id=$1`, u.ID).
		Scan(&u.DepartmentID, &u.SiteScope, &u.DelegateUserID, &u.DelegateUntil, &u.ApprovalDelegate)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	csrf, _ := r.Context().Value(csrfContextKey).(string)
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "csrfToken": csrf, "version": map[string]string{"version": s.version, "commit": s.commit, "builtAt": s.builtAt}})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, s.keys.Digest(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if csrf, _ := r.Context().Value(csrfContextKey).(string); csrf == "" {
		writeError(w, http.StatusForbidden, "browser_session_required", "비밀번호 변경에는 브라우저 세션이 필요합니다")
		return
	}
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.NewPassword) < 12 {
		writeError(w, http.StatusBadRequest, "weak_password", "새 비밀번호는 12자 이상이어야 합니다")
		return
	}
	u, _ := userFrom(r)
	var hash, source string
	if err := s.db.QueryRow(r.Context(), `SELECT COALESCE(password_hash,''),source FROM users WHERE id=$1`, u.ID).Scan(&hash, &source); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if source != "local" || hash == "" {
		writeError(w, http.StatusBadRequest, "oidc_user", "SSO 사용자의 비밀번호는 Keycloak에서 변경하세요")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.CurrentPassword)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid_password", "현재 비밀번호가 일치하지 않습니다")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), 12)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	_, err = s.db.Exec(r.Context(), `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, u.ID, string(newHash))
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "auth.password_change", "user", u.ID, r.RemoteAddr, nil)
	_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1 AND token_hash<>$2`, u.ID, currentSessionDigest(r, s.keys))
	w.WriteHeader(http.StatusNoContent)
}

func currentSessionDigest(r *http.Request, keys *platform.Keyring) []byte {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return keys.Digest(c.Value)
	}
	return nil
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	if enabled, _ := s.getSetting(r.Context(), "oidc.enabled"); enabled != "true" {
		writeError(w, http.StatusNotFound, "oidc_disabled", "SSO가 설정되지 않았습니다")
		return
	}
	issuer, _ := s.getSetting(r.Context(), "oidc.issuer_url")
	clientID, _ := s.getSetting(r.Context(), "oidc.client_id")
	secret, _ := s.getSetting(r.Context(), "oidc.client_secret")
	provider, err := oidc.NewProvider(r.Context(), issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "oidc_discovery_failed", "Keycloak 연결을 확인하세요")
		return
	}
	state, _ := platform.RandomToken(32)
	nonce, _ := platform.RandomToken(24)
	verifier := oauth2.GenerateVerifier()
	returnTo := r.URL.Query().Get("returnTo")
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		returnTo = "/"
	}
	_, err = s.db.Exec(r.Context(), `INSERT INTO oidc_states(state_hash,nonce,verifier,return_to,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, s.keys.Digest(state), nonce, verifier, returnTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "oidc_state_failed", "SSO 요청을 시작하지 못했습니다")
		return
	}
	cfg := oauth2.Config{ClientID: clientID, ClientSecret: secret, Endpoint: provider.Endpoint(), RedirectURL: s.publicBaseURL(r.Context(), r) + "/api/v1/auth/oidc/callback", Scopes: s.oidcScopes(r.Context())}
	http.Redirect(w, r, cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, "/login?error="+url.QueryEscape(e), http.StatusFound)
		return
	}
	state := r.URL.Query().Get("state")
	var nonce, verifier, returnTo string
	err := s.db.QueryRow(r.Context(), `DELETE FROM oidc_states WHERE state_hash=$1 AND expires_at>now() RETURNING nonce,verifier,return_to`, s.keys.Digest(state)).Scan(&nonce, &verifier, &returnTo)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_oidc_state", "SSO 요청이 만료되었거나 유효하지 않습니다")
		return
	}
	issuer, _ := s.getSetting(r.Context(), "oidc.issuer_url")
	clientID, _ := s.getSetting(r.Context(), "oidc.client_id")
	secret, _ := s.getSetting(r.Context(), "oidc.client_secret")
	provider, err := oidc.NewProvider(r.Context(), issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "oidc_discovery_failed", "Keycloak 연결을 확인하세요")
		return
	}
	cfg := oauth2.Config{ClientID: clientID, ClientSecret: secret, Endpoint: provider.Endpoint(), RedirectURL: s.publicBaseURL(r.Context(), r) + "/api/v1/auth/oidc/callback", Scopes: s.oidcScopes(r.Context())}
	token, err := cfg.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "oidc_exchange_failed", "SSO 인증 코드를 확인하지 못했습니다")
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing_id_token", "Keycloak ID 토큰이 없습니다")
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: clientID}).Verify(r.Context(), rawID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_id_token", "Keycloak ID 토큰 검증에 실패했습니다")
		return
	}
	var claims struct {
		Subject           string   `json:"sub"`
		Nonce             string   `json:"nonce"`
		PreferredUsername string   `json:"preferred_username"`
		Name              string   `json:"name"`
		Email             string   `json:"email"`
		PhoneNumber       string   `json:"phone_number"`
		Groups            []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil || claims.Subject == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_oidc_claims", "Keycloak 토큰 요청값이 일치하지 않습니다")
		return
	}
	username := claims.PreferredUsername
	if username == "" {
		username = claims.Email
	}
	if username == "" {
		username = "oidc-" + claims.Subject
	}
	name := claims.Name
	if name == "" {
		name = username
	}
	role := s.roleForGroups(r.Context(), claims.Groups)
	autoProvision, _ := s.getSetting(r.Context(), "oidc.auto_provision")
	if autoProvision != "true" {
		var exists bool
		if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE active AND source='oidc' AND ((oidc_issuer=$1 AND oidc_subject=$2) OR (oidc_subject IS NULL AND lower(username)=lower($3))))`, issuer, claims.Subject, username).Scan(&exists); err != nil || !exists {
			writeError(w, http.StatusForbidden, "oidc_provision_disabled", "등록된 사용자만 SSO로 로그인할 수 있습니다")
			return
		}
	}
	id, err := s.upsertOIDCUser(r.Context(), issuer, claims.Subject, username, name, claims.Email, role)
	if conflict, ok := err.(oidcIdentityConflict); ok {
		writeError(w, http.StatusConflict, "oidc_identity_conflict", conflict.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user_provision_failed", "SSO 사용자를 등록하지 못했습니다")
		return
	}
	if strings.TrimSpace(claims.PhoneNumber) != "" {
		if encryptedPhone, encryptErr := s.keys.Encrypt(normalizePhone(claims.PhoneNumber)); encryptErr == nil {
			_, _ = s.db.Exec(r.Context(), `UPDATE users SET phone_encrypted=$2 WHERE id=$1`, id, encryptedPhone)
		}
	}
	u, err := scanUser(s.db.QueryRow(r.Context(), `SELECT id,username,display_name,COALESCE(email,''),employee_id,role,source,last_login_at FROM users WHERE id=$1`, id))
	if err != nil || s.issueSession(w, r, u) != nil {
		writeError(w, http.StatusInternalServerError, "session_error", "로그인 세션을 만들지 못했습니다")
		return
	}
	s.audit(r.Context(), u.ID, "auth.login", "user", u.ID, r.RemoteAddr, map[string]string{"source": "oidc"})
	http.Redirect(w, r, returnTo, http.StatusFound)
}

type oidcIdentityConflict string

func (e oidcIdentityConflict) Error() string { return string(e) }

func (s *Server) upsertOIDCUser(ctx context.Context, issuer, subject, username, name, email, role string) (string, error) {
	var id, source, boundSubject string
	err := s.db.QueryRow(ctx, `SELECT id,source,COALESCE(oidc_subject,'') FROM users WHERE oidc_issuer=$1 AND oidc_subject=$2`, issuer, subject).Scan(&id, &source, &boundSubject)
	if err == pgx.ErrNoRows {
		err = s.db.QueryRow(ctx, `SELECT id,source,COALESCE(oidc_subject,'') FROM users WHERE lower(username)=lower($1)`, username).Scan(&id, &source, &boundSubject)
		if err == nil && (source != "oidc" || boundSubject != "") {
			return "", oidcIdentityConflict("같은 아이디의 다른 인증 계정이 있습니다. 관리자에게 OIDC 계정 연결을 요청하세요")
		}
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}
	if id == "" {
		id = newID()
		_, err = s.db.Exec(ctx, `INSERT INTO users(id,username,display_name,email,role,source,oidc_issuer,oidc_subject,last_login_at) VALUES($1,$2,$3,$4,$5,'oidc',$6,$7,now())`, id, username, name, email, role, issuer, subject)
		return id, err
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET display_name=$2,email=NULLIF($3,''),source='oidc',oidc_issuer=$4,oidc_subject=$5,last_login_at=now(),role=CASE WHEN role_override OR role='super_admin' THEN role ELSE $6 END,updated_at=now() WHERE id=$1`, id, name, email, issuer, subject, role)
	return id, err
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host
}

func (s *Server) oidcScopes(ctx context.Context) []string {
	v, _ := s.getSetting(ctx, "oidc.scopes")
	parts := strings.Fields(v)
	if len(parts) == 0 {
		return []string{oidc.ScopeOpenID, "profile", "email"}
	}
	return parts
}

func (s *Server) roleForGroups(ctx context.Context, groups []string) string {
	admin, _ := s.getSetting(ctx, "oidc.admin_group")
	lobby, _ := s.getSetting(ctx, "oidc.lobby_group")
	security, _ := s.getSetting(ctx, "oidc.security_group")
	auditor, _ := s.getSetting(ctx, "oidc.auditor_group")
	manager, _ := s.getSetting(ctx, "oidc.department_manager_group")
	for _, g := range groups {
		if admin != "" && g == admin {
			return RoleAdmin
		}
		if security != "" && g == security {
			return RoleSecurity
		}
		if auditor != "" && g == auditor {
			return RoleAuditor
		}
		if lobby != "" && g == lobby {
			return RoleLobby
		}
	}
	for _, g := range groups {
		if manager != "" && g == manager {
			return RoleDeptManager
		}
	}
	return RoleUser
}

func (s *Server) testOIDC(w http.ResponseWriter, r *http.Request) {
	issuer, _ := s.getSetting(r.Context(), "oidc.issuer_url")
	if issuer == "" {
		writeError(w, http.StatusBadRequest, "issuer_required", "Issuer URL을 입력하세요")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "oidc_discovery_failed", fmt.Sprintf("Discovery 실패: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "issuer": issuer, "authorizationEndpoint": provider.Endpoint().AuthURL, "tokenEndpoint": provider.Endpoint().TokenURL, "redirectUri": s.publicBaseURL(r.Context(), r) + "/api/v1/auth/oidc/callback"})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,username,display_name,COALESCE(email,''),employee_id,role,source,last_login_at,active,department_id,site_scope,role_override FROM users ORDER BY display_name`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var u User
		var active, roleOverride bool
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.EmployeeID, &u.Role, &u.Source, &u.LastLoginAt, &active, &u.DepartmentID, &u.SiteScope, &roleOverride); err == nil {
			users = append(users, map[string]any{"id": u.ID, "username": u.Username, "displayName": u.DisplayName, "email": u.Email, "employeeId": u.EmployeeID, "role": u.Role, "source": u.Source, "lastLoginAt": u.LastLoginAt, "active": active, "departmentId": u.DepartmentID, "siteScope": u.SiteScope, "roleOverride": roleOverride})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role           string    `json:"role"`
		Active         *bool     `json:"active"`
		DepartmentID   *string   `json:"departmentId"`
		SiteScope      *[]string `json:"siteScope"`
		UseOIDCMapping *bool     `json:"useOidcMapping"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	allowed := map[string]bool{RoleUser: true, RoleLobby: true, RoleDeptManager: true, RoleSecurity: true, RoleAuditor: true, RoleAdmin: true, RoleSuperAdmin: true}
	if in.Role != "" && !allowed[in.Role] {
		writeError(w, 400, "invalid_role", "권한 값이 올바르지 않습니다")
		return
	}
	id := chiURLParam(r, "userID")
	var beforeRole string
	var beforeActive, beforeRoleOverride bool
	var beforeDepartment *string
	var beforeSites []string
	if err := s.db.QueryRow(r.Context(), `SELECT role,active,department_id,site_scope,role_override FROM users WHERE id=$1`, id).Scan(&beforeRole, &beforeActive, &beforeDepartment, &beforeSites, &beforeRoleOverride); err != nil {
		notFoundOrServer(w, err)
		return
	}
	afterRole, afterActive := beforeRole, beforeActive
	if in.Role != "" {
		afterRole = in.Role
	}
	if in.Active != nil {
		afterActive = *in.Active
	}
	if beforeRole == RoleSuperAdmin && (afterRole != RoleSuperAdmin || !afterActive) {
		var superAdmins int
		if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE role='super_admin' AND active`).Scan(&superAdmins); err != nil {
			notFoundOrServer(w, err)
			return
		}
		if superAdmins <= 1 {
			writeError(w, http.StatusConflict, "last_super_admin", "마지막 최고 관리자는 권한을 낮추거나 비활성화할 수 없습니다")
			return
		}
	}
	departmentSet := in.DepartmentID != nil
	department := ""
	if departmentSet {
		department = *in.DepartmentID
	}
	sitesSet := in.SiteScope != nil
	sites := beforeSites
	if sitesSet {
		sites = *in.SiteScope
	}
	roleOverride := beforeRoleOverride
	if in.Role != "" {
		roleOverride = true
	}
	if in.UseOIDCMapping != nil && *in.UseOIDCMapping {
		roleOverride = false
	}
	_, err := s.db.Exec(r.Context(), `UPDATE users SET role=COALESCE(NULLIF($2,''),role),active=COALESCE($3,active),department_id=CASE WHEN $4 THEN NULLIF($5,'') ELSE department_id END,site_scope=CASE WHEN $6 THEN $7 ELSE site_scope END,role_override=$8,updated_at=now() WHERE id=$1`, id, in.Role, in.Active, departmentSet, department, sitesSet, sites, roleOverride)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "user.update", "user", id, r.RemoteAddr, map[string]any{
		"before": map[string]any{"role": beforeRole, "active": beforeActive, "departmentId": beforeDepartment, "siteScope": beforeSites, "roleOverride": beforeRoleOverride},
		"after":  map[string]any{"role": afterRole, "active": afterActive, "departmentId": in.DepartmentID, "siteScope": sites, "roleOverride": roleOverride},
	})
	w.WriteHeader(http.StatusNoContent)
}

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
