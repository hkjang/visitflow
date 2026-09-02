package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// settingsCacheTTL bounds how stale a setting may be on a node that did not
// perform the change itself. The node that saves settings invalidates at once.
const settingsCacheTTL = 5 * time.Second

type cachedSetting struct {
	value   string
	err     error
	expires time.Time
}

// getSetting is on the hot path of authentication, QR verification and visit
// creation, each of which reads several settings; the cache turns those into
// one database round trip per key every few seconds instead of per request.
func (s *Server) getSetting(ctx context.Context, key string) (string, error) {
	if s.db == nil {
		return "", errors.New("database is not configured")
	}
	now := time.Now()
	s.settingsMu.RLock()
	entry, ok := s.settingsCache[key]
	s.settingsMu.RUnlock()
	if ok && now.Before(entry.expires) {
		return entry.value, entry.err
	}
	var value string
	var secret bool
	err := s.db.QueryRow(ctx, `SELECT value,secret FROM settings WHERE key=$1`, key).Scan(&value, &secret)
	if err == nil && secret && value != "" {
		value, err = s.keys.Decrypt(value)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// Transient database errors are not cached; a missing key is.
		return "", err
	}
	s.settingsMu.Lock()
	if s.settingsCache == nil {
		s.settingsCache = map[string]cachedSetting{}
	}
	s.settingsCache[key] = cachedSetting{value: value, err: err, expires: now.Add(settingsCacheTTL)}
	s.settingsMu.Unlock()
	return value, err
}

func (s *Server) invalidateSettings() {
	s.settingsMu.Lock()
	s.settingsCache = nil
	s.settingsMu.Unlock()
}

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) {
	// security.key_check is an internal canary, not an operator setting.
	rows, err := s.db.Query(r.Context(), `SELECT key,value,secret FROM settings WHERE key<>'security.key_check' ORDER BY key`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []Setting{}
	for rows.Next() {
		var item Setting
		if err := rows.Scan(&item.Key, &item.Value, &item.Secret); err != nil {
			continue
		}
		item.Configured = item.Value != ""
		if item.Secret && item.Configured {
			item.Value = "********"
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Settings map[string]string `json:"settings"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	for key, value := range in.Settings {
		if value == "********" {
			continue
		}
		if message := validateSettingValue(key, strings.TrimSpace(value)); message != "" {
			writeError(w, http.StatusBadRequest, "invalid_setting", message)
			return
		}
	}
	effective := func(key string) string {
		if value, ok := in.Settings[key]; ok && value != "********" {
			return strings.TrimSpace(value)
		}
		value, _ := s.getSetting(r.Context(), key)
		return strings.TrimSpace(value)
	}
	if effective("auth.local_enabled") != "true" && effective("oidc.enabled") != "true" {
		writeError(w, http.StatusBadRequest, "authentication_lockout", "로컬 로그인 또는 Keycloak SSO 중 하나는 활성화해야 합니다")
		return
	}
	if effective("oidc.enabled") == "true" && (effective("oidc.issuer_url") == "" || effective("oidc.client_id") == "" || effective("oidc.client_secret") == "") {
		writeError(w, http.StatusBadRequest, "oidc_incomplete", "SSO 활성화에는 Issuer URL, Client ID, Client Secret이 필요합니다")
		return
	}
	if effective("notification.provider") == "webhook" && effective("notification.webhook_url") == "" {
		writeError(w, http.StatusBadRequest, "webhook_url_required", "webhook Provider에는 Webhook URL이 필요합니다")
		return
	}
	supported := map[string]bool{}
	for _, locale := range strings.Fields(effective("general.supported_locales")) {
		supported[normalizeLocale(locale)] = true
	}
	if defaultLocale := normalizeLocale(effective("general.default_locale")); defaultLocale != "" && len(supported) > 0 && !supported[defaultLocale] {
		writeError(w, http.StatusBadRequest, "locale_not_supported", "기본 언어는 지원 언어 목록에 포함되어야 합니다")
		return
	}
	maskDays, _ := strconv.Atoi(effective("privacy.mask_after_days"))
	destroyDays, _ := strconv.Atoi(effective("privacy.destroy_after_days"))
	if maskDays >= destroyDays {
		writeError(w, http.StatusBadRequest, "invalid_privacy_period", "개인정보 마스킹 시점은 파기 시점보다 빨라야 합니다")
		return
	}
	keys := make([]string, 0, len(in.Settings))
	for key := range in.Settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	updated := []string{}
	changes := map[string]any{}
	for _, key := range keys {
		value := strings.TrimSpace(in.Settings[key])
		var secret bool
		var current string
		if err := tx.QueryRow(r.Context(), `SELECT secret,value FROM settings WHERE key=$1`, key).Scan(&secret, &current); err != nil {
			if err == pgx.ErrNoRows {
				writeError(w, http.StatusBadRequest, "unknown_setting", "지원하지 않는 설정입니다: "+key)
				return
			}
			notFoundOrServer(w, err)
			return
		}
		if secret && value == "********" {
			continue
		}
		if secret && current == "" && value == "" {
			continue
		}
		if !secret && value == current {
			continue
		}
		before, after := current, value
		if secret && value != "" {
			value, err = s.keys.Encrypt(value)
			if err != nil {
				notFoundOrServer(w, err)
				return
			}
		}
		if secret {
			before = map[bool]string{true: "configured", false: "empty"}[current != ""]
			after = map[bool]string{true: "configured", false: "empty"}[value != ""]
		}
		u, _ := userFrom(r)
		if _, err = tx.Exec(r.Context(), `UPDATE settings SET value=$2,updated_at=now(),updated_by=$3 WHERE key=$1`, key, value, u.ID); err != nil {
			notFoundOrServer(w, err)
			return
		}
		updated = append(updated, key)
		changes[key] = map[string]string{"before": before, "after": after}
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.invalidateSettings()
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "settings.update", "settings", "", r.RemoteAddr, map[string]any{"changes": changes})
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

func validateSettingValue(key, value string) string {
	if key == "security.key_check" {
		return "암호화 키 검증 값은 변경할 수 없습니다"
	}
	booleans := map[string]bool{
		"auth.local_enabled": true, "oidc.enabled": true, "oidc.auto_provision": true,
		"visit.approval_enabled": true, "visit.single_use_qr": true, "visit.company_required": true,
		"visit.self_registration_enabled": true,
	}
	if booleans[key] && value != "true" && value != "false" {
		return key + " 값은 true 또는 false여야 합니다"
	}
	ranges := map[string][2]int{
		"security.session_hours": {1, 720}, "security.api_key_days": {1, 3650},
		"security.rotation_grace_hours": {0, 168}, "security.api_key_max_active": {1, 100},
		"visit.early_checkin_minutes": {0, 1440}, "visit.late_grace_minutes": {0, 1440},
		"visit.auto_checkout_hour": {0, 23}, "privacy.mask_after_days": {1, 3650},
		"privacy.destroy_after_days": {2, 7300}, "privacy.audit_retention_days": {1, 7300},
		"security.login_max_attempts": {1, 100}, "security.login_lockout_minutes": {1, 1440},
		"security.public_rate_limit_per_minute": {1, 100000},
		"visit.approval_escalation_hours":       {1, 8760}, "visit.self_registration_hours": {1, 720},
	}
	if bounds, ok := ranges[key]; ok {
		n, err := strconv.Atoi(value)
		if err != nil || n < bounds[0] || n > bounds[1] {
			return key + " 값의 허용 범위를 확인하세요"
		}
	}
	if key == "visit.dynamic_qr_seconds" {
		n, err := strconv.Atoi(value)
		if err != nil || (n != 0 && (n < 30 || n > 60)) {
			return "Dynamic QR 주기는 0(비활성) 또는 30~60초여야 합니다"
		}
	}
	if key == "general.default_locale" && value != "" && normalizeLocale(value) == "" {
		return "기본 언어는 ko, en, ja, zh 중 하나여야 합니다"
	}
	if key == "general.supported_locales" {
		parts := strings.Fields(value)
		if len(parts) == 0 {
			return "지원 언어는 하나 이상 입력해야 합니다"
		}
		for _, locale := range parts {
			if normalizeLocale(locale) == "" {
				return "지원하지 않는 언어 코드입니다: " + locale
			}
		}
	}
	if key == "privacy.consent_policy_version" && (value == "" || len(value) > 32) {
		return "동의 정책 버전은 1~32자로 입력하세요"
	}
	if key == "notification.provider" && value != "log" && value != "webhook" {
		return "알림 Provider는 log 또는 webhook이어야 합니다"
	}
	if key == "security.api_key_allowed_scopes" {
		parts := strings.Fields(value)
		seen := map[string]bool{}
		if len(parts) == 0 || len(parts) > 3 {
			return "허용 키 범위는 read, write, mcp 중 하나 이상이어야 합니다"
		}
		for _, scope := range parts {
			if seen[scope] || (scope != "read" && scope != "write" && scope != "mcp") {
				return "허용 키 범위는 read, write, mcp를 공백으로 구분해 입력하세요"
			}
			seen[scope] = true
		}
	}
	if key == "general.base_url" || key == "oidc.issuer_url" || key == "notification.webhook_url" {
		if value == "" {
			return ""
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return key + " 값은 http(s) URL이어야 합니다"
		}
	}
	return ""
}

// exportSettings produces a file that PUT /settings accepts verbatim, so a
// configuration can be carried to another site or restored after a rebuild.
// Secrets are never exported: they belong to the installation's key.
func (s *Server) exportSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT key,value FROM settings WHERE NOT secret AND key<>'security.key_check' ORDER BY key`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) == nil {
			values[key] = value
		}
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "settings.export", "settings", "", r.RemoteAddr, map[string]any{"count": len(values)})
	w.Header().Set("Content-Disposition", `attachment; filename="visitflow-settings.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"format": "visitflow-settings/1", "exportedAt": time.Now(), "version": s.version, "settings": values})
}
