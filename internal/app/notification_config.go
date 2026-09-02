package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/net/http/httpguts"
)

const maskedNotificationSecret = "********"

var (
	notificationTemplatePattern = regexp.MustCompile(`\{\{([A-Za-z][A-Za-z0-9]*)\}\}`)
	templateKeyPattern          = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	errNotificationAPIDisabled  = errors.New("선택한 문자 API가 비활성화되었습니다")
)

// notificationChannels covers both messaging and the generic 'webhook' channel
// used to drive gates, guest Wi-Fi and other systems from the same adapter.
var notificationChannels = map[string]bool{"sms": true, "mms": true, "kakao": true, "webhook": true}

var notificationEvents = map[string]bool{
	"visit_confirmed": true, "visit_start": true, "checked_in": true,
	"checked_out": true, "visit_cancelled": true, "visit_rejected": true, "approval_escalated": true,
}

var notificationTemplateKeys = map[string]bool{
	"recipient": true, "message": true, "channel": true, "idempotencyKey": true, "notificationId": true,
	"company": true, "visitorCompany": true, "visitor": true, "host": true, "start": true, "end": true,
	"place": true, "lobby": true, "checkedIn": true, "checkedOut": true, "requestNo": true,
	"passUrl": true, "qrcodeFileSeq": true, "qrcodePath": true, "qrcodeUrl": true, "visitId": true, "visitorVisitId": true,
	"locale": true, "visitType": true, "badgeNo": true, "siteCode": true, "delegate": true,
}

type notificationAPIInput struct {
	Name           string            `json:"name"`
	Channel        string            `json:"channel"`
	BaseURL        string            `json:"baseUrl"`
	Path           string            `json:"path"`
	Method         string            `json:"method"`
	RequestFormat  string            `json:"requestFormat"`
	Headers        map[string]string `json:"headers"`
	Parameters     map[string]string `json:"parameters"`
	SecretKeys     []string          `json:"secretKeys"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Enabled        *bool             `json:"enabled"`
}

type notificationAPIConfig struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Channel        string            `json:"channel"`
	BaseURL        string            `json:"baseUrl"`
	Path           string            `json:"path"`
	Method         string            `json:"method"`
	RequestFormat  string            `json:"requestFormat"`
	Headers        map[string]string `json:"headers"`
	Parameters     map[string]string `json:"parameters"`
	SecretKeys     []string          `json:"secretKeys"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Enabled        bool              `json:"enabled"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

type notificationRuleInput struct {
	Name          string `json:"name"`
	Event         string `json:"event"`
	Audience      string `json:"audience"`
	Channel       string `json:"channel"`
	APIConfigID   string `json:"apiConfigId"`
	OffsetMinutes int    `json:"offsetMinutes"`
	TemplateKey   string `json:"templateKey"`
	BodyTemplate  string `json:"bodyTemplate"`
	Locale        string `json:"locale"`
	Enabled       *bool  `json:"enabled"`
}

type notificationRule struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Event         string    `json:"event"`
	Audience      string    `json:"audience"`
	Channel       string    `json:"channel"`
	APIConfigID   string    `json:"apiConfigId,omitempty"`
	APIConfigName string    `json:"apiConfigName,omitempty"`
	OffsetMinutes int       `json:"offsetMinutes"`
	TemplateKey   string    `json:"templateKey"`
	BodyTemplate  string    `json:"bodyTemplate"`
	Locale        string    `json:"locale"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func normalizeNotificationAPIInput(in *notificationAPIInput) {
	in.Name = strings.TrimSpace(in.Name)
	in.Channel = strings.ToLower(strings.TrimSpace(in.Channel))
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.Path = strings.TrimSpace(in.Path)
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	in.RequestFormat = strings.ToLower(strings.TrimSpace(in.RequestFormat))
	if in.Method == "" {
		in.Method = http.MethodPost
	}
	if in.RequestFormat == "" {
		in.RequestFormat = "json"
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 10
	}
	if in.Headers == nil {
		in.Headers = map[string]string{}
	}
	if in.Parameters == nil {
		in.Parameters = map[string]string{}
	}
	seen := map[string]bool{}
	keys := make([]string, 0, len(in.SecretKeys))
	for _, key := range in.SecretKeys {
		key = strings.TrimSpace(key)
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	in.SecretKeys = keys
}

func notificationAPIInputEnabled(in notificationAPIInput) bool {
	return in.Enabled == nil || *in.Enabled
}

func notificationValuesUseIdempotency(headers, parameters map[string]string) bool {
	for _, values := range []map[string]string{headers, parameters} {
		for _, value := range values {
			if strings.Contains(value, "{{idempotencyKey}}") || strings.Contains(value, "{{notificationId}}") {
				return true
			}
		}
	}
	return false
}

func notificationAPIUsesIdempotency(in notificationAPIInput) bool {
	return notificationValuesUseIdempotency(in.Headers, in.Parameters)
}

func validateNotificationAPIInput(in notificationAPIInput) string {
	if in.Name == "" || len(in.Name) > 100 {
		return "API 이름은 1~100자로 입력하세요"
	}
	if !notificationChannels[in.Channel] {
		return "채널은 sms, mms, kakao, webhook 중 하나여야 합니다"
	}
	parsed, err := url.Parse(in.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "기본 URL은 사용자 정보, query, fragment가 없는 http(s) URL이어야 합니다"
	}
	if strings.Contains(in.Path, "://") || strings.ContainsAny(in.Path, "?#") || strings.HasPrefix(in.Path, "//") {
		return "API path에는 별도 호스트, query, fragment를 사용할 수 없습니다"
	}
	if in.Path != "" && !strings.HasPrefix(in.Path, "/") {
		return "API path는 /로 시작해야 합니다"
	}
	if in.Method != http.MethodGet && in.Method != http.MethodPost && in.Method != http.MethodPut && in.Method != http.MethodPatch {
		return "HTTP method는 GET, POST, PUT, PATCH 중 하나여야 합니다"
	}
	if in.RequestFormat != "json" && in.RequestFormat != "form" && in.RequestFormat != "query" {
		return "요청 형식은 json, form, query 중 하나여야 합니다"
	}
	if in.Method == http.MethodGet && in.RequestFormat != "query" {
		return "GET API는 query 요청 형식을 사용해야 합니다"
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 60 {
		return "Timeout은 1~60초여야 합니다"
	}
	if len(in.Headers) > 50 || len(in.Parameters) > 100 {
		return "Header는 50개, Parameter는 100개까지 등록할 수 있습니다"
	}
	for key, value := range in.Headers {
		if !httpguts.ValidHeaderFieldName(key) || strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") || len(value) > 10000 {
			return "허용되지 않는 Header 이름 또는 값이 있습니다"
		}
		if strings.ContainsAny(value, "\r\n") {
			return "Header 값에는 줄바꿈을 사용할 수 없습니다"
		}
		if message := validateNotificationTemplate(value); message != "" {
			return "Header " + key + ": " + message
		}
	}
	for key, value := range in.Parameters {
		if strings.TrimSpace(key) == "" || len(key) > 128 || len(value) > 50000 {
			return "Parameter 이름 또는 값의 길이를 확인하세요"
		}
		if message := validateNotificationTemplate(value); message != "" {
			return "Parameter " + key + ": " + message
		}
	}
	for _, secret := range in.SecretKeys {
		section, key := splitNotificationSecretKey(secret)
		_, headerExists := in.Headers[key]
		_, parameterExists := in.Parameters[key]
		if (section == "headers" && !headerExists) || (section == "parameters" && !parameterExists) || (section == "" && !headerExists && !parameterExists) {
			return "Secret Key가 Header 또는 Parameter에 없습니다: " + secret
		}
	}
	if notificationAPIInputEnabled(in) && !notificationAPIUsesIdempotency(in) {
		return "활성 API의 Header 또는 Parameter에 {{idempotencyKey}} 또는 {{notificationId}}를 사용해야 합니다"
	}
	return ""
}

func normalizeNotificationRuleInput(in *notificationRuleInput) {
	in.Name = strings.TrimSpace(in.Name)
	in.Event = strings.ToLower(strings.TrimSpace(in.Event))
	in.Audience = strings.ToLower(strings.TrimSpace(in.Audience))
	in.Channel = strings.ToLower(strings.TrimSpace(in.Channel))
	in.APIConfigID = strings.TrimSpace(in.APIConfigID)
	in.TemplateKey = strings.ToLower(strings.TrimSpace(in.TemplateKey))
	in.BodyTemplate = strings.TrimSpace(in.BodyTemplate)
	in.Locale = strings.ToLower(strings.TrimSpace(in.Locale))
	// Store the base tag the visitor record uses ("ko-KR" would never match "ko").
	if normalized := normalizeLocale(in.Locale); normalized != "" {
		in.Locale = normalized
	}
}

func validateNotificationRuleInput(in notificationRuleInput) string {
	if in.Name == "" || len(in.Name) > 100 {
		return "규칙 이름은 1~100자로 입력하세요"
	}
	if !notificationEvents[in.Event] {
		return "지원하지 않는 발송 시점입니다"
	}
	if in.Audience != "visitor" && in.Audience != "host" && in.Audience != "system" {
		return "수신 대상은 visitor, host 또는 system이어야 합니다"
	}
	if !notificationChannels[in.Channel] {
		return "채널은 sms, mms, kakao, webhook 중 하나여야 합니다"
	}
	if in.Audience == "system" && in.Channel != "webhook" {
		return "system 대상 규칙은 webhook 채널을 사용해야 합니다"
	}
	if in.Audience == "system" && in.APIConfigID == "" {
		return "system 대상 규칙에는 호출할 외부 API를 선택해야 합니다"
	}
	if in.Locale != "" && normalizeLocale(in.Locale) == "" {
		return "지원하지 않는 언어 코드입니다"
	}
	if in.OffsetMinutes < -10080 || in.OffsetMinutes > 10080 || (in.Event != "visit_start" && in.OffsetMinutes < 0) {
		return "방문 시작 전 예약만 음수 Offset을 사용할 수 있으며 범위는 ±10080분입니다"
	}
	if !templateKeyPattern.MatchString(in.TemplateKey) {
		return "Template Key는 영문 소문자, 숫자, 점, 밑줄, 하이픈만 사용할 수 있습니다"
	}
	if in.BodyTemplate == "" || len(in.BodyTemplate) > 50000 {
		return "본문 템플릿은 1~50000자로 입력하세요"
	}
	if message := validateNotificationTemplate(in.BodyTemplate); message != "" {
		return message
	}
	for _, match := range notificationTemplatePattern.FindAllStringSubmatch(in.BodyTemplate, -1) {
		if match[1] == "message" || match[1] == "idempotencyKey" || match[1] == "notificationId" {
			return "메시지 본문에서 사용할 수 없는 변수입니다: {{" + match[1] + "}}"
		}
	}
	return ""
}

func validateNotificationTemplate(value string) string {
	for _, match := range notificationTemplatePattern.FindAllStringSubmatch(value, -1) {
		if !notificationTemplateKeys[match[1]] {
			return "지원하지 않는 템플릿 변수입니다: {{" + match[1] + "}}"
		}
	}
	withoutKnown := notificationTemplatePattern.ReplaceAllString(value, "")
	if strings.Contains(withoutKnown, "{{") || strings.Contains(withoutKnown, "}}") {
		return "템플릿 변수 형식을 확인하세요"
	}
	return ""
}

func renderNotificationTemplate(value string, variables map[string]string) (string, error) {
	if message := validateNotificationTemplate(value); message != "" {
		return "", errors.New(message)
	}
	var missing string
	result := notificationTemplatePattern.ReplaceAllStringFunc(value, func(token string) string {
		match := notificationTemplatePattern.FindStringSubmatch(token)
		resolved, ok := variables[match[1]]
		if !ok {
			missing = match[1]
			return ""
		}
		return resolved
	})
	if missing != "" {
		return "", fmt.Errorf("템플릿 값이 없습니다: %s", missing)
	}
	return result, nil
}

func splitNotificationSecretKey(value string) (string, string) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) == 2 && (parts[0] == "headers" || parts[0] == "parameters") {
		return parts[0], parts[1]
	}
	return "", value
}

func notificationValueIsSecret(secretKeys []string, section, key string) bool {
	for _, value := range secretKeys {
		candidateSection, candidateKey := splitNotificationSecretKey(value)
		if candidateKey == key && (candidateSection == "" || candidateSection == section) {
			return true
		}
	}
	return false
}

func maskNotificationSecrets(values map[string]string, secretKeys []string, section string) map[string]string {
	masked := make(map[string]string, len(values))
	for key, value := range values {
		if notificationValueIsSecret(secretKeys, section, key) && value != "" {
			value = maskedNotificationSecret
		}
		masked[key] = value
	}
	return masked
}

func mergeNotificationSecrets(values, previous map[string]string, secretKeys []string, section string) (map[string]string, string) {
	merged := make(map[string]string, len(values))
	for key, value := range values {
		if value == maskedNotificationSecret {
			previousValue, exists := previous[key]
			if !notificationValueIsSecret(secretKeys, section, key) || !exists || previousValue == "" {
				return nil, "마스킹된 값은 기존 Secret Key에만 사용할 수 있습니다: " + section + "." + key
			}
			value = previousValue
		}
		merged[key] = value
	}
	return merged, ""
}

func (s *Server) encodeNotificationMap(values map[string]string) (string, error) {
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return s.keys.Encrypt(string(b))
}

func (s *Server) decodeNotificationMap(value string) (map[string]string, error) {
	if value == "" {
		return map[string]string{}, nil
	}
	plain, err := s.keys.Decrypt(value)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) scanNotificationAPI(row pgx.Row, revealSecrets bool) (notificationAPIConfig, error) {
	var item notificationAPIConfig
	var headersEncrypted, parametersEncrypted string
	var secretJSON []byte
	err := row.Scan(&item.ID, &item.Name, &item.Channel, &item.BaseURL, &item.Path, &item.Method, &item.RequestFormat, &headersEncrypted, &parametersEncrypted, &secretJSON, &item.TimeoutSeconds, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal(secretJSON, &item.SecretKeys); err != nil {
		return item, err
	}
	if item.Headers, err = s.decodeNotificationMap(headersEncrypted); err != nil {
		return item, err
	}
	if item.Parameters, err = s.decodeNotificationMap(parametersEncrypted); err != nil {
		return item, err
	}
	if !revealSecrets {
		item.Headers = maskNotificationSecrets(item.Headers, item.SecretKeys, "headers")
		item.Parameters = maskNotificationSecrets(item.Parameters, item.SecretKeys, "parameters")
	}
	return item, nil
}

const notificationAPISelect = `SELECT id,name,channel,base_url,path,method,request_format,headers_encrypted,parameters_encrypted,secret_keys,timeout_seconds,enabled,created_at,updated_at FROM notification_api_configs`

func (s *Server) listNotificationAPIs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), notificationAPISelect+` ORDER BY channel,name`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []notificationAPIConfig{}
	for rows.Next() {
		item, scanErr := s.scanNotificationAPI(rows, false)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "notification_config_error", "문자 API 설정을 복호화하지 못했습니다")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createNotificationAPI(w http.ResponseWriter, r *http.Request) {
	var in notificationAPIInput
	if !decodeJSON(w, r, &in) {
		return
	}
	normalizeNotificationAPIInput(&in)
	if message := validateNotificationAPIInput(in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_api", message)
		return
	}
	for _, values := range []map[string]string{in.Headers, in.Parameters} {
		for _, value := range values {
			if value == maskedNotificationSecret {
				writeError(w, http.StatusBadRequest, "invalid_notification_api", "새 API의 비밀값에는 마스킹 문자열을 사용할 수 없습니다")
				return
			}
		}
	}
	headerEncrypted, err := s.encodeNotificationMap(in.Headers)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	parametersEncrypted, err := s.encodeNotificationMap(in.Parameters)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	secretJSON, _ := json.Marshal(in.SecretKeys)
	id := newID()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	u, _ := userFrom(r)
	_, err = s.db.Exec(r.Context(), `INSERT INTO notification_api_configs(id,name,channel,base_url,path,method,request_format,headers_encrypted,parameters_encrypted,secret_keys,timeout_seconds,enabled,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, id, in.Name, in.Channel, in.BaseURL, in.Path, in.Method, in.RequestFormat, headerEncrypted, parametersEncrypted, secretJSON, in.TimeoutSeconds, enabled, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "notification_api.create", "notification_api", id, r.RemoteAddr, map[string]any{"name": in.Name, "channel": in.Channel, "secretCount": len(in.SecretKeys)})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) updateNotificationAPI(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "apiID")
	var in notificationAPIInput
	if !decodeJSON(w, r, &in) {
		return
	}
	normalizeNotificationAPIInput(&in)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	previous, err := s.scanNotificationAPI(tx.QueryRow(r.Context(), notificationAPISelect+` WHERE id=$1 FOR UPDATE`, id), true)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	var mergeMessage string
	in.Headers, mergeMessage = mergeNotificationSecrets(in.Headers, previous.Headers, in.SecretKeys, "headers")
	if mergeMessage == "" {
		in.Parameters, mergeMessage = mergeNotificationSecrets(in.Parameters, previous.Parameters, in.SecretKeys, "parameters")
	}
	if mergeMessage != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_api", mergeMessage)
		return
	}
	if message := validateNotificationAPIInput(in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_api", message)
		return
	}
	var mismatch int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM notification_rules WHERE api_config_id=$1 AND channel<>$2`, id, in.Channel).Scan(&mismatch); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if mismatch > 0 {
		writeError(w, http.StatusConflict, "notification_api_in_use", "이 API를 사용하는 규칙의 채널과 일치하지 않습니다")
		return
	}
	headerEncrypted, err := s.encodeNotificationMap(in.Headers)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	parametersEncrypted, err := s.encodeNotificationMap(in.Parameters)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	secretJSON, _ := json.Marshal(in.SecretKeys)
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tag, err := tx.Exec(r.Context(), `UPDATE notification_api_configs SET name=$2,channel=$3,base_url=$4,path=$5,method=$6,request_format=$7,headers_encrypted=$8,parameters_encrypted=$9,secret_keys=$10,timeout_seconds=$11,enabled=$12,updated_at=now() WHERE id=$1`, id, in.Name, in.Channel, in.BaseURL, in.Path, in.Method, in.RequestFormat, headerEncrypted, parametersEncrypted, secretJSON, in.TimeoutSeconds, enabled)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "문자 API 설정을 찾을 수 없습니다")
		return
	}
	disabledRules := int64(0)
	cancelledPending := int64(0)
	if !enabled {
		ruleTag, disableErr := tx.Exec(r.Context(), `UPDATE notification_rules SET enabled=false,updated_at=now() WHERE api_config_id=$1 AND enabled`, id)
		if disableErr != nil {
			notFoundOrServer(w, disableErr)
			return
		}
		disabledRules = ruleTag.RowsAffected()
		pendingTag, cancelErr := tx.Exec(r.Context(), `UPDATE notifications
			SET status='cancelled',attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,error=NULL,claimed_at=NULL,claim_token=NULL
			WHERE api_config_id=$1 AND status IN ('queued','failed','sending')`, id)
		if cancelErr != nil {
			notFoundOrServer(w, cancelErr)
			return
		}
		cancelledPending = pendingTag.RowsAffected()
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification_api.update", "notification_api", id, r.RemoteAddr, map[string]any{"name": in.Name, "channel": in.Channel, "enabled": enabled, "secretCount": len(in.SecretKeys), "disabledRules": disabledRules, "cancelledPending": cancelledPending})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteNotificationAPI(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "apiID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lockedID string
	if err = tx.QueryRow(r.Context(), `SELECT id FROM notification_api_configs WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	var references int
	if err = tx.QueryRow(r.Context(), `SELECT
		(SELECT count(*) FROM notification_rules WHERE api_config_id=$1)+
		(SELECT count(*) FROM notifications WHERE api_config_id=$1 AND status IN ('queued','failed','sending'))`, id).Scan(&references); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if references > 0 {
		writeError(w, http.StatusConflict, "notification_api_in_use", "먼저 이 API를 사용하는 발송 규칙을 변경하거나 삭제하세요")
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM notification_api_configs WHERE id=$1`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "문자 API 설정을 찾을 수 없습니다")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification_api.delete", "notification_api", id, r.RemoteAddr, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listNotificationRules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT nr.id,nr.name,nr.event,nr.audience,nr.channel,COALESCE(nr.api_config_id,''),COALESCE(na.name,''),nr.offset_minutes,nr.template_key,nr.body_template,nr.locale,nr.enabled,nr.created_at,nr.updated_at FROM notification_rules nr LEFT JOIN notification_api_configs na ON na.id=nr.api_config_id ORDER BY nr.event,nr.offset_minutes,nr.name`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []notificationRule{}
	for rows.Next() {
		var item notificationRule
		if err := rows.Scan(&item.ID, &item.Name, &item.Event, &item.Audience, &item.Channel, &item.APIConfigID, &item.APIConfigName, &item.OffsetMinutes, &item.TemplateKey, &item.BodyTemplate, &item.Locale, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			notFoundOrServer(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) validateRuleAPITx(ctx context.Context, tx pgx.Tx, in notificationRuleInput, ruleEnabled bool) string {
	if in.APIConfigID == "" {
		return ""
	}
	api, err := s.scanNotificationAPI(tx.QueryRow(ctx, notificationAPISelect+` WHERE id=$1 FOR KEY SHARE`, in.APIConfigID), true)
	if err != nil {
		return "선택한 문자 API를 찾을 수 없습니다"
	}
	if api.Channel != in.Channel {
		return "발송 규칙과 문자 API의 채널이 일치해야 합니다"
	}
	if ruleEnabled && !api.Enabled {
		return "활성 발송 규칙은 활성 문자 API만 선택할 수 있습니다"
	}
	if ruleEnabled && !notificationValuesUseIdempotency(api.Headers, api.Parameters) {
		return "활성 발송 규칙은 멱등성 변수가 설정된 문자 API만 선택할 수 있습니다"
	}
	return ""
}

func (s *Server) createNotificationRule(w http.ResponseWriter, r *http.Request) {
	var in notificationRuleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	normalizeNotificationRuleInput(&in)
	if message := validateNotificationRuleInput(in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_rule", message)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	id := newID()
	u, _ := userFrom(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if message := s.validateRuleAPITx(r.Context(), tx, in, enabled); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_rule", message)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO notification_rules(id,name,event,audience,channel,api_config_id,offset_minutes,template_key,body_template,locale,enabled,created_by) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12)`, id, in.Name, in.Event, in.Audience, in.Channel, in.APIConfigID, in.OffsetMinutes, in.TemplateKey, in.BodyTemplate, in.Locale, enabled, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "notification_rule.create", "notification_rule", id, r.RemoteAddr, map[string]any{"event": in.Event, "audience": in.Audience, "channel": in.Channel, "apiConfigId": in.APIConfigID, "offsetMinutes": in.OffsetMinutes})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) updateNotificationRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ruleID")
	var in notificationRuleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	normalizeNotificationRuleInput(&in)
	if message := validateNotificationRuleInput(in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_rule", message)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if message := s.validateRuleAPITx(r.Context(), tx, in, enabled); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_notification_rule", message)
		return
	}
	var oldEvent, oldAudience, oldChannel, oldAPIConfigID, oldTemplateKey, oldBodyTemplate, oldLocale string
	var oldOffsetMinutes int
	var oldEnabled bool
	if err = tx.QueryRow(r.Context(), `SELECT event,audience,channel,COALESCE(api_config_id,''),offset_minutes,template_key,body_template,locale,enabled FROM notification_rules WHERE id=$1 FOR UPDATE`, id).
		Scan(&oldEvent, &oldAudience, &oldChannel, &oldAPIConfigID, &oldOffsetMinutes, &oldTemplateKey, &oldBodyTemplate, &oldLocale, &oldEnabled); err != nil {
		notFoundOrServer(w, err)
		return
	}
	queuePolicyChanged := oldEvent != in.Event || oldAudience != in.Audience || oldChannel != in.Channel || oldAPIConfigID != in.APIConfigID || oldOffsetMinutes != in.OffsetMinutes || oldTemplateKey != in.TemplateKey || oldBodyTemplate != in.BodyTemplate || oldLocale != in.Locale || oldEnabled != enabled
	tag, err := tx.Exec(r.Context(), `UPDATE notification_rules SET name=$2,event=$3,audience=$4,channel=$5,api_config_id=NULLIF($6,''),offset_minutes=$7,template_key=$8,body_template=$9,locale=$10,enabled=$11,updated_at=now() WHERE id=$1`, id, in.Name, in.Event, in.Audience, in.Channel, in.APIConfigID, in.OffsetMinutes, in.TemplateKey, in.BodyTemplate, in.Locale, enabled)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "발송 규칙을 찾을 수 없습니다")
		return
	}
	cancelledPending := int64(0)
	refreshedVisits := 0
	if !enabled {
		cancelled, cancelErr := tx.Exec(r.Context(), `UPDATE notifications
			SET status='cancelled',attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,error=NULL,claimed_at=NULL,claim_token=NULL
			WHERE rule_id=$1 AND status IN ('queued','failed','sending')`, id)
		if cancelErr != nil {
			notFoundOrServer(w, cancelErr)
			return
		}
		cancelledPending = cancelled.RowsAffected()
	} else if queuePolicyChanged && oldEvent == "visit_start" && in.Event == "visit_start" {
		if _, err = tx.Exec(r.Context(), `UPDATE notifications
			SET status='queued',attempts=GREATEST(attempts-1,0),next_attempt_at=now(),error=NULL,claimed_at=NULL,claim_token=NULL
			WHERE rule_id=$1 AND status='sending'`, id); err != nil {
			notFoundOrServer(w, err)
			return
		}
		visitIDs := []string{}
		rows, queryErr := tx.Query(r.Context(), `SELECT DISTINCT visit_id FROM notifications WHERE rule_id=$1 AND visit_id IS NOT NULL AND status IN ('queued','failed') ORDER BY visit_id`, id)
		if queryErr != nil {
			notFoundOrServer(w, queryErr)
			return
		}
		for rows.Next() {
			var visitID string
			if scanErr := rows.Scan(&visitID); scanErr != nil {
				rows.Close()
				notFoundOrServer(w, scanErr)
				return
			}
			visitIDs = append(visitIDs, visitID)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			notFoundOrServer(w, rowsErr)
			return
		}
		rows.Close()
		for _, visitID := range visitIDs {
			if err = s.refreshVisitStartRuleNotificationsTx(r.Context(), tx, visitID, id); err != nil {
				notFoundOrServer(w, err)
				return
			}
			refreshedVisits++
		}
	} else if queuePolicyChanged {
		cancelled, cancelErr := tx.Exec(r.Context(), `UPDATE notifications
			SET status='cancelled',attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,error=NULL,claimed_at=NULL,claim_token=NULL
			WHERE rule_id=$1 AND status IN ('queued','failed','sending')`, id)
		if cancelErr != nil {
			notFoundOrServer(w, cancelErr)
			return
		}
		cancelledPending = cancelled.RowsAffected()
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification_rule.update", "notification_rule", id, r.RemoteAddr, map[string]any{"event": in.Event, "audience": in.Audience, "channel": in.Channel, "apiConfigId": in.APIConfigID, "offsetMinutes": in.OffsetMinutes, "enabled": enabled, "cancelledPending": cancelledPending, "refreshedVisits": refreshedVisits})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteNotificationRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ruleID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lockedID string
	if err = tx.QueryRow(r.Context(), `SELECT id FROM notification_rules WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	cancelled, err := tx.Exec(r.Context(), `UPDATE notifications
		SET status='cancelled',attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,error=NULL,claimed_at=NULL,claim_token=NULL
		WHERE rule_id=$1 AND status IN ('queued','failed','sending')`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM notification_rules WHERE id=$1`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "발송 규칙을 찾을 수 없습니다")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "notification_rule.delete", "notification_rule", id, r.RemoteAddr, map[string]int64{"cancelledPending": cancelled.RowsAffected()})
	w.WriteHeader(http.StatusNoContent)
}

type notificationEventData struct {
	VisitID, VisitorVisitID, RequestNo, Visitor, VisitorPhone, VisitorCompany             string
	Host, HostPhone, Company, Place, Lobby, PassURL, QRCodeFileSeq, QRCodePath, QRCodeURL string
	Locale, VisitType, BadgeNo, SiteCode, Delegate                                        string
	StartAt, EndAt                                                                        time.Time
	Timezone                                                                              string
}

func (s *Server) notificationEventDataTx(ctx context.Context, tx pgx.Tx, visitID, visitorVisitID string) (notificationEventData, error) {
	var data notificationEventData
	var visitorNameEncrypted, visitorPhoneEncrypted, hostPhoneEncrypted, delegatePhoneEncrypted, tokenEncrypted string
	err := tx.QueryRow(ctx, `SELECT v.id,vv.id,v.request_no,p.name_encrypted,p.phone_encrypted,COALESCE(p.company,''),h.display_name,COALESCE(h.phone_encrypted,''),COALESCE(sv.value,''),COALESCE(NULLIF(v.place_detail,''),s.name),COALESCE(l.name,''),v.start_at,v.end_at,s.timezone,COALESCE(q.token_encrypted,''),COALESCE(q.qrcode_file_seq,''),
		p.locale,COALESCE(vt.name,''),COALESCE(vv.badge_no,''),s.code,COALESCE(d.display_name,''),COALESCE(d.phone_encrypted,'')
		FROM visits v JOIN visitor_visits vv ON vv.visit_id=v.id JOIN visitors p ON p.id=vv.visitor_id JOIN users h ON h.id=v.host_user_id JOIN sites s ON s.id=v.site_id
		LEFT JOIN lobbies l ON l.id=v.lobby_id LEFT JOIN settings sv ON sv.key='general.company_name'
		LEFT JOIN visit_types vt ON vt.id=v.visit_type_id
		LEFT JOIN users d ON d.id=h.delegate_user_id AND h.delegate_until>now() AND d.active
		LEFT JOIN LATERAL (SELECT token_encrypted,qrcode_file_seq FROM qr_tokens WHERE visitor_visit_id=vv.id AND revoked_at IS NULL ORDER BY issued_at DESC LIMIT 1) q ON true
		WHERE v.id=$1 AND vv.id=$2`, visitID, visitorVisitID).Scan(&data.VisitID, &data.VisitorVisitID, &data.RequestNo, &visitorNameEncrypted, &visitorPhoneEncrypted, &data.VisitorCompany, &data.Host, &hostPhoneEncrypted, &data.Company, &data.Place, &data.Lobby, &data.StartAt, &data.EndAt, &data.Timezone, &tokenEncrypted, &data.QRCodeFileSeq,
		&data.Locale, &data.VisitType, &data.BadgeNo, &data.SiteCode, &data.Delegate, &delegatePhoneEncrypted)
	if err != nil {
		return data, err
	}
	data.Visitor = s.decryptOptional(visitorNameEncrypted)
	data.VisitorPhone = s.decryptOptional(visitorPhoneEncrypted)
	data.HostPhone = s.decryptOptional(hostPhoneEncrypted)
	// While a host has an active delegate, arrival notifications reach the
	// colleague who is actually available to meet the visitor.
	if delegatePhone := s.decryptOptional(delegatePhoneEncrypted); delegatePhone != "" {
		data.HostPhone = delegatePhone
	}
	base := s.publicBaseURL(ctx, nil)
	if tokenEncrypted != "" {
		data.PassURL = strings.TrimRight(base, "/") + "/q/" + s.decryptOptional(tokenEncrypted)
	}
	if data.QRCodeFileSeq != "" {
		data.QRCodePath = "/img/visitor/" + data.QRCodeFileSeq + ".jpg"
		data.QRCodeURL = strings.TrimRight(base, "/") + data.QRCodePath
	}
	return data, nil
}

func (data notificationEventData) variables(eventAt time.Time) map[string]string {
	location := time.Local
	if configured, err := time.LoadLocation(data.Timezone); err == nil {
		location = configured
	}
	return map[string]string{
		"company": data.Company, "visitorCompany": data.VisitorCompany, "visitor": data.Visitor, "host": data.Host,
		"start": data.StartAt.In(location).Format("2006-01-02 15:04"), "end": data.EndAt.In(location).Format("2006-01-02 15:04"),
		"place": data.Place, "lobby": data.Lobby, "checkedIn": eventAt.In(location).Format("15:04"), "checkedOut": eventAt.In(location).Format("15:04"),
		"requestNo": data.RequestNo, "passUrl": data.PassURL, "qrcodeFileSeq": data.QRCodeFileSeq, "qrcodePath": data.QRCodePath, "qrcodeUrl": data.QRCodeURL,
		"visitId": data.VisitID, "visitorVisitId": data.VisitorVisitID,
		"locale": data.Locale, "visitType": data.VisitType, "badgeNo": data.BadgeNo, "siteCode": data.SiteCode, "delegate": data.Delegate,
	}
}

// queueNotificationEventTx materializes every enabled rule for one visit participant.
// visit_start rules are materialized when the visit is confirmed and remain queued
// until start_at+offset; all other rules are relative to the event timestamp.
func (s *Server) queueNotificationEventTx(ctx context.Context, tx pgx.Tx, visitID, visitorVisitID, event string, eventAt time.Time) error {
	return s.queueNotificationEventCountTx(ctx, tx, visitID, visitorVisitID, event, eventAt, nil)
}

func (s *Server) queueNotificationEventCountTx(ctx context.Context, tx pgx.Tx, visitID, visitorVisitID, event string, eventAt time.Time, queued *int) error {
	data, err := s.notificationEventDataTx(ctx, tx, visitID, visitorVisitID)
	if err != nil {
		return err
	}
	type queuedRule struct {
		id, audience, channel, apiConfigID, templateKey, bodyTemplate, locale string
		offsetMinutes                                                         int
	}
	rules := []queuedRule{}
	// A rule with a locale only fires for visitors who chose that language, so
	// one event can carry a Korean and an English template side by side.
	rows, err := tx.Query(ctx, `SELECT id,audience,channel,COALESCE(api_config_id,''),offset_minutes,template_key,body_template,locale
		FROM notification_rules WHERE enabled AND event=$1 AND (locale='' OR locale=$2) ORDER BY created_at`, event, data.Locale)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rule queuedRule
		if err := rows.Scan(&rule.id, &rule.audience, &rule.channel, &rule.apiConfigID, &rule.offsetMinutes, &rule.templateKey, &rule.bodyTemplate, &rule.locale); err != nil {
			rows.Close()
			return err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	// Personal e-mail alerts run beside the administrator's messaging rules.
	if err := s.queueHostMailTx(ctx, tx, data, event, eventAt); err != nil {
		return err
	}
	variables := data.variables(eventAt)
	metadataJSON, _ := json.Marshal(variables)
	metadataEncrypted, err := s.keys.Encrypt(string(metadataJSON))
	if err != nil {
		return err
	}
	for _, rule := range rules {
		recipient := notificationRecipient(rule.audience, data)
		if strings.TrimSpace(recipient) == "" {
			continue
		}
		variables["recipient"] = recipient
		variables["channel"] = rule.channel
		body, renderErr := renderNotificationTemplate(rule.bodyTemplate, variables)
		if renderErr != nil {
			return renderErr
		}
		recipientEncrypted, encryptErr := s.keys.Encrypt(recipient)
		if encryptErr != nil {
			return encryptErr
		}
		bodyEncrypted, encryptErr := s.keys.Encrypt(body)
		if encryptErr != nil {
			return encryptErr
		}
		scheduledAt := eventAt.Add(time.Duration(rule.offsetMinutes) * time.Minute)
		if event == "visit_start" {
			scheduledAt = data.StartAt.Add(time.Duration(rule.offsetMinutes) * time.Minute)
		}
		if scheduledAt.Before(time.Now()) {
			scheduledAt = time.Now()
		}
		_, err = tx.Exec(ctx, `INSERT INTO notifications(id,visit_id,visitor_visit_id,rule_id,api_config_id,recipient_encrypted,channel,template_key,body_encrypted,metadata_encrypted,next_attempt_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11)`, newID(), visitID, visitorVisitID, rule.id, rule.apiConfigID, recipientEncrypted, rule.channel, rule.templateKey, bodyEncrypted, metadataEncrypted, scheduledAt)
		if err != nil {
			return err
		}
		if queued != nil {
			(*queued)++
		}
	}
	return nil
}

// notificationRecipient resolves what the adapter is addressed to. Messaging
// audiences use a normalized phone number; a 'system' rule targets an external
// system, so the participant id identifies the subject instead.
func notificationRecipient(audience string, data notificationEventData) string {
	switch audience {
	case "host":
		return normalizePhone(data.HostPhone)
	case "system":
		return data.VisitorVisitID
	default:
		return normalizePhone(data.VisitorPhone)
	}
}

func (s *Server) cancelPendingVisitNotificationsTx(ctx context.Context, tx pgx.Tx, visitID string) error {
	_, err := tx.Exec(ctx, `UPDATE notifications
		SET status='cancelled',attempts=CASE WHEN status='sending' THEN GREATEST(attempts-1,0) ELSE attempts END,error=NULL,claimed_at=NULL,claim_token=NULL
		WHERE visit_id=$1 AND status IN ('queued','failed','sending')`, visitID)
	return err
}

func cancelPendingNotificationTx(ctx context.Context, tx pgx.Tx, notificationID, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE notifications SET status='cancelled',error=NULLIF($2,''),claimed_at=NULL,claim_token=NULL WHERE id=$1 AND status IN ('queued','failed')`, notificationID, reason)
	return err
}

// refreshVisitStartNotificationsTx applies the current rule as one complete
// snapshot. With no participant argument it refreshes the whole visit; QR
// reissue passes one visitor_visit_id so unrelated retries are not reset.
func (s *Server) refreshVisitStartNotificationsTx(ctx context.Context, tx pgx.Tx, visitID string, visitorVisitIDs ...string) error {
	participantScope := ""
	if len(visitorVisitIDs) > 0 {
		participantScope = strings.TrimSpace(visitorVisitIDs[0])
	}
	return s.refreshVisitStartNotificationsScopedTx(ctx, tx, visitID, participantScope, "")
}

func (s *Server) refreshVisitStartRuleNotificationsTx(ctx context.Context, tx pgx.Tx, visitID, ruleID string) error {
	return s.refreshVisitStartNotificationsScopedTx(ctx, tx, visitID, "", ruleID)
}

func (s *Server) refreshVisitStartNotificationsScopedTx(ctx context.Context, tx pgx.Tx, visitID, participantScope, ruleScope string) error {
	type pendingNotification struct {
		id, participantID, bodyTemplate, audience, channel, apiConfigID, templateKey, locale string
		offsetMinutes                                                                        int
		enabled                                                                              bool
	}
	items := []pendingNotification{}
	rows, err := tx.Query(ctx, `SELECT n.id,COALESCE(n.visitor_visit_id,''),nr.body_template,nr.audience,nr.channel,COALESCE(nr.api_config_id,''),nr.offset_minutes,nr.template_key,nr.locale,nr.enabled
		FROM notifications n JOIN notification_rules nr ON nr.id=n.rule_id
		WHERE n.visit_id=$1 AND ($2='' OR n.visitor_visit_id=$2) AND ($3='' OR n.rule_id=$3)
		AND nr.event='visit_start' AND n.status IN ('queued','failed') FOR UPDATE OF n`, visitID, participantScope, ruleScope)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item pendingNotification
		if err := rows.Scan(&item.id, &item.participantID, &item.bodyTemplate, &item.audience, &item.channel, &item.apiConfigID, &item.offsetMinutes, &item.templateKey, &item.locale, &item.enabled); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		if !item.enabled {
			if err := cancelPendingNotificationTx(ctx, tx, item.id, "발송 규칙이 비활성화되었습니다"); err != nil {
				return err
			}
			continue
		}
		if item.participantID == "" {
			if err := cancelPendingNotificationTx(ctx, tx, item.id, "방문자 방문 정보가 없습니다"); err != nil {
				return err
			}
			continue
		}
		if item.apiConfigID != "" {
			var apiChannel string
			var apiEnabled bool
			apiErr := tx.QueryRow(ctx, `SELECT channel,enabled FROM notification_api_configs WHERE id=$1`, item.apiConfigID).Scan(&apiChannel, &apiEnabled)
			if apiErr != nil && !errors.Is(apiErr, pgx.ErrNoRows) {
				return apiErr
			}
			if errors.Is(apiErr, pgx.ErrNoRows) || !apiEnabled || apiChannel != item.channel {
				if err := cancelPendingNotificationTx(ctx, tx, item.id, "문자 API가 없거나 비활성화되었습니다"); err != nil {
					return err
				}
				continue
			}
		}
		data, dataErr := s.notificationEventDataTx(ctx, tx, visitID, item.participantID)
		if dataErr != nil {
			return dataErr
		}
		variables := data.variables(time.Now())
		if item.locale != "" && item.locale != data.Locale {
			if err := cancelPendingNotificationTx(ctx, tx, item.id, "방문자 언어가 발송 규칙과 일치하지 않습니다"); err != nil {
				return err
			}
			continue
		}
		recipient := notificationRecipient(item.audience, data)
		if recipient == "" {
			if err := cancelPendingNotificationTx(ctx, tx, item.id, "수신 번호가 없습니다"); err != nil {
				return err
			}
			continue
		}
		variables["recipient"] = recipient
		variables["channel"] = item.channel
		body, renderErr := renderNotificationTemplate(item.bodyTemplate, variables)
		if renderErr != nil {
			return renderErr
		}
		metadataJSON, _ := json.Marshal(variables)
		metadataEncrypted, encryptErr := s.keys.Encrypt(string(metadataJSON))
		if encryptErr != nil {
			return encryptErr
		}
		bodyEncrypted, encryptErr := s.keys.Encrypt(body)
		if encryptErr != nil {
			return encryptErr
		}
		recipientEncrypted, encryptErr := s.keys.Encrypt(recipient)
		if encryptErr != nil {
			return encryptErr
		}
		nextAttemptAt := data.StartAt.Add(time.Duration(item.offsetMinutes) * time.Minute)
		if nextAttemptAt.Before(time.Now()) {
			nextAttemptAt = time.Now()
		}
		_, err = tx.Exec(ctx, `UPDATE notifications
			SET recipient_encrypted=$2,channel=$3,api_config_id=NULLIF($4,''),template_key=$5,body_encrypted=$6,metadata_encrypted=$7,next_attempt_at=$8,
			status='queued',attempts=0,error=NULL,provider_message_id=NULL,sent_at=NULL,claimed_at=NULL,claim_token=NULL
			WHERE id=$1 AND status IN ('queued','failed')`, item.id, recipientEncrypted, item.channel, item.apiConfigID, item.templateKey, bodyEncrypted, metadataEncrypted, nextAttemptAt)
		if err != nil {
			return err
		}
	}
	return nil
}

type claimedNotification struct {
	ID, Recipient, Message, Channel, APIConfigID string
	Metadata                                     map[string]string
}

func joinNotificationURL(baseURL, apiPath string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	parsed.Path = basePath + "/" + strings.TrimLeft(apiPath, "/")
	if apiPath == "" {
		parsed.Path = basePath
	}
	parsed.RawPath = ""
	return parsed.String(), nil
}

func buildNotificationRequest(ctx context.Context, config notificationAPIConfig, item claimedNotification) (*http.Request, error) {
	endpoint, err := joinNotificationURL(config.BaseURL, config.Path)
	if err != nil {
		return nil, err
	}
	variables := make(map[string]string, len(item.Metadata)+5)
	for key, value := range item.Metadata {
		variables[key] = value
	}
	variables["recipient"] = item.Recipient
	variables["message"] = item.Message
	variables["channel"] = item.Channel
	variables["idempotencyKey"] = item.ID
	variables["notificationId"] = item.ID
	parameters := make(map[string]string, len(config.Parameters))
	for key, value := range config.Parameters {
		parameters[key], err = renderNotificationTemplate(value, variables)
		if err != nil {
			return nil, fmt.Errorf("parameter %s: %w", key, err)
		}
	}
	var body io.Reader
	if config.RequestFormat == "query" {
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil {
			return nil, parseErr
		}
		query := parsed.Query()
		for key, value := range parameters {
			query.Set(key, value)
		}
		parsed.RawQuery = query.Encode()
		endpoint = parsed.String()
	} else if config.RequestFormat == "form" {
		form := url.Values{}
		for key, value := range parameters {
			form.Set(key, value)
		}
		body = strings.NewReader(form.Encode())
	} else {
		encoded, encodeErr := json.Marshal(parameters)
		if encodeErr != nil {
			return nil, encodeErr
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, config.Method, endpoint, body)
	if err != nil {
		return nil, err
	}
	for key, value := range config.Headers {
		rendered, renderErr := renderNotificationTemplate(value, variables)
		if renderErr != nil {
			return nil, fmt.Errorf("header %s: %w", key, renderErr)
		}
		req.Header.Set(key, rendered)
	}
	if req.Header.Get("Content-Type") == "" {
		switch config.RequestFormat {
		case "json":
			req.Header.Set("Content-Type", "application/json")
		case "form":
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	return req, nil
}

func providerMessageID(header http.Header, body []byte) string {
	for _, key := range []string{"X-Message-ID", "X-Request-ID"} {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return truncateNotificationError(value, 200)
		}
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		for _, key := range []string{"messageId", "message_id", "id"} {
			if value, ok := payload[key]; ok {
				return truncateNotificationError(fmt.Sprint(value), 200)
			}
		}
	}
	return ""
}

func truncateNotificationError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func (s *Server) loadNotificationAPI(ctx context.Context, id string) (notificationAPIConfig, error) {
	item, err := s.scanNotificationAPI(s.db.QueryRow(ctx, notificationAPISelect+` WHERE id=$1`, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errors.New("선택한 문자 API가 없습니다")
	}
	if err != nil {
		return item, err
	}
	if !item.Enabled {
		return item, errNotificationAPIDisabled
	}
	return item, nil
}

func parseNotificationMetadata(s *Server, encrypted string) (map[string]string, error) {
	if encrypted == "" {
		return map[string]string{}, nil
	}
	plain, err := s.keys.Decrypt(encrypted)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func notificationBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 5 {
		attempts = 5
	}
	return time.Duration(attempts*5) * time.Minute
}
